// Command wes is the composition root: it wires config from env into
// adapters, use cases, and the HTTP router, then serves.
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	inboundhttp "github.com/claudioed/wes-work-planning/internal/adapters/inbound/http"
	inboundkafka "github.com/claudioed/wes-work-planning/internal/adapters/inbound/kafka"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/events"
	outboundkafka "github.com/claudioed/wes-work-planning/internal/adapters/outbound/kafka"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/memory"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/postgres"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/productclassification"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/telemetry"
	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/application/usecases"
)

// serviceName is this service's identity in OTel resource attributes and
// span/metric scopes; OTEL_SERVICE_NAME can override it.
const serviceName = "wes-work-planning"

func main() {
	if err := run(); err != nil {
		slog.Error("service exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	logger := newLogger(getenv("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	httpAddr := getenv("HTTP_ADDR", ":8080")
	databaseURL := os.Getenv("DATABASE_URL")
	kafkaBrokers := os.Getenv("KAFKA_BROKERS")
	eventPublisherKind := getenv("EVENT_PUBLISHER", "log")
	otelServiceName := getenv("OTEL_SERVICE_NAME", serviceName)

	shutdownTelemetry, err := telemetry.Setup(
		context.Background(),
		otelServiceName,
		getenv("SERVICE_VERSION", telemetry.DefaultServiceVersion),
		getenv("OTEL_EXPORTER_OTLP_ENDPOINT", telemetry.DefaultEndpoint),
	)
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// A failed final flush means the Collector was unreachable, not that
		// the service failed — log it and let the process exit cleanly.
		if err := shutdownTelemetry(ctx); err != nil {
			logger.Warn("telemetry shutdown did not flush cleanly", "error", err)
		}
	}()

	clock := memory.SystemClock{}

	var (
		charges   ports.ChargeRepo
		plans     ports.PlanRepo
		pools     ports.WorkPoolRepo
		workUnits ports.WorkUnitRepo

		laborPlanViews ports.LaborPlanViewRepo
		inventoryViews ports.InventoryViewRepo
		processedEvts  ports.ProcessedEventRepo
	)

	if databaseURL == "" {
		logger.Info("database url not configured; using in-memory adapters")
		charges = memory.NewChargeRepo()
		plans = memory.NewPlanRepo()
		pools = memory.NewWorkPoolRepo()
		workUnits = memory.NewWorkUnitRepo()
		laborPlanViews = memory.NewLaborPlanViewRepo()
		inventoryViews = memory.NewInventoryViewRepo()
		processedEvts = memory.NewProcessedEventRepo()
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		pool, err := postgres.Connect(ctx, databaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()

		charges = postgres.NewChargeRepo(pool)
		plans = postgres.NewPlanRepo(pool)
		pools = postgres.NewWorkPoolRepo(pool)
		workUnits = postgres.NewWorkUnitRepo(pool)
		laborPlanViews = postgres.NewLaborPlanViewRepo(pool)
		inventoryViews = postgres.NewInventoryViewRepo(pool)
		processedEvts = postgres.NewProcessedEventRepo(pool)
	}

	var publisher ports.EventPublisher
	classifications := buildClassificationLookup(getenv("PRODUCT_CLASSIFICATION_MODE", "permissive"), os.Getenv("INVENTORY_STORAGE_BASE_URL"), logger)
	switch eventPublisherKind {
	case "kafka":
		if kafkaBrokers == "" {
			return fmt.Errorf("EVENT_PUBLISHER=kafka requires KAFKA_BROKERS to be set")
		}
		logger.Info("event publisher configured", "publisher", "kafka", "brokers", kafkaBrokers)
		kafkaPublisher := outboundkafka.NewPublisher(brokerList(kafkaBrokers), workUnits, classifications, newEventID)
		defer func() { _ = kafkaPublisher.Close() }()
		publisher = kafkaPublisher
	default:
		publisher = events.NewLogPublisher(logger)
	}

	recordCompletion := usecases.NewRecordCompletion(workUnits, publisher, clock)

	handlers := &inboundhttp.Handlers{
		ReceiveChargeForecast: usecases.NewReceiveChargeForecast(charges, publisher, clock),
		CommitShiftPlan:       usecases.NewCommitShiftPlan(plans, publisher, clock),
		EnqueueWorkUnit:       usecases.NewEnqueueWorkUnit(workUnits, pools, publisher, clock),
		ReleaseNextWork:       usecases.NewReleaseNextWork(pools, workUnits, publisher, clock),
		RecordCompletion:      recordCompletion,
		SampleBacklog:         usecases.NewSampleBacklog(pools, publisher, clock),
		RebalanceDecision:     usecases.NewRebalanceDecision(pools, publisher, clock),
		LaborPlanView:         usecases.NewLaborPlanView(laborPlanViews),
		InventoryView:         usecases.NewInventoryView(inventoryViews),
	}

	router := inboundhttp.NewRouter(handlers, otelServiceName, logger)

	server := &http.Server{
		Addr:              httpAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", httpAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	var consumer *inboundkafka.Consumer
	consumerCtx, cancelConsumer := context.WithCancel(context.Background())
	defer cancelConsumer()

	if kafkaBrokers != "" {
		logger.Info("consuming integration events", "brokers", kafkaBrokers)
		observeLabor := usecases.NewObserveLaborPlan(laborPlanViews, processedEvts)
		observeInventory := usecases.NewObserveInventoryChange(inventoryViews, processedEvts)
		consumer = inboundkafka.NewConsumer(brokerList(kafkaBrokers), "wes-work-planning", observeLabor, observeInventory, recordCompletion, processedEvts, logger)
		go func() {
			if err := consumer.Run(consumerCtx); err != nil {
				logger.Error("kafka consumer stopped", "error", err)
			}
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case <-sigCh:
		logger.Info("shutting down")
		cancelConsumer()
		if consumer != nil {
			_ = consumer.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	}
}

// newLogger builds the process-wide structured logger: JSON to stdout, at
// the level LOG_LEVEL names (debug|info|warn|error, case-insensitive,
// default info). Records are routed through telemetry.TraceHandler so any
// log emitted inside a span carries that span's trace_id and span_id.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(telemetry.NewTraceHandler(handler))
}

func brokerList(csv string) []string {
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// newEventID generates a UUID v4 for outbound integration event envelopes.
func newEventID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// buildClassificationLookup selects the outbound
// ports.ProductClassificationLookup adapter via PRODUCT_CLASSIFICATION_MODE
// (http|permissive), defaulting to "permissive" so existing tests, CI and
// deployments that do not set the env var are unaffected — mirroring
// inventory-storage's own LOCATION_LOOKUP_MODE=http|permissive pattern (see
// ADR-0009). "http" requires INVENTORY_STORAGE_BASE_URL.
func buildClassificationLookup(mode, inventoryStorageBaseURL string, logger *slog.Logger) ports.ProductClassificationLookup {
	if !strings.EqualFold(mode, "http") {
		return productclassification.NewPermissiveLookup()
	}
	logger.Info("product classification lookup configured", "mode", "http", "inventory_storage_base_url", inventoryStorageBaseURL)
	return productclassification.NewClient(inventoryStorageBaseURL, nil)
}
