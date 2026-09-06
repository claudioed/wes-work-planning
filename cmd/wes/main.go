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
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/filecatalog"
	outboundkafka "github.com/claudioed/wes-work-planning/internal/adapters/outbound/kafka"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/kafkacatalog"
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
	migrationsPath := getenv("MIGRATIONS_PATH", "migrations")
	kafkaBrokers := os.Getenv("KAFKA_BROKERS")
	eventPublisherKind := getenv("EVENT_PUBLISHER", "log")
	otelServiceName := getenv("OTEL_SERVICE_NAME", serviceName)

	// The process-path catalogue's SOURCE is selectable, defaulting to
	// the existing boot-time file read ("file") -- zero behavior change
	// for any existing deployment unless PATH_CATALOGUE_SOURCE=kafka is
	// explicitly set, matching this fleet's EVENT_PUBLISHER convention.
	// See internal/adapters/outbound/kafkacatalog's package doc comment
	// for the full rationale and the readiness-gate design, mirrored
	// byte-for-byte from fulfillment-execution's identical wiring.
	catalogueSource := getenv("PATH_CATALOGUE_SOURCE", "file")

	var catalogue ports.PathCatalogue
	var kafkaCatalogue *kafkacatalog.Consumer
	// catalogueConsumerCtx/cancelCatalogueConsumer are declared here
	// (rather than at cancelConsumer's original location further down)
	// because the Kafka catalogue source needs its own Run goroutine
	// started BEFORE WaitReady is called below -- otherwise nothing
	// would ever be consuming messages while this process waits,
	// guaranteeing a deadlock until WaitReadyTimeout.
	catalogueConsumerCtx, cancelCatalogueConsumer := context.WithCancel(context.Background())
	defer cancelCatalogueConsumer()

	switch catalogueSource {
	case "kafka":
		if kafkaBrokers == "" {
			return fmt.Errorf("PATH_CATALOGUE_SOURCE=kafka requires KAFKA_BROKERS to be set")
		}
		var err error
		kafkaCatalogue, err = kafkacatalog.NewConsumer(context.Background(), brokerList(kafkaBrokers), logger)
		if err != nil {
			return fmt.Errorf("failed to start the Kafka-sourced process-path catalogue: %w", err)
		}
		logger.Info("process-path catalogue source configured", "source", "kafka", "topic", kafkacatalog.Topic)
		go func() {
			logger.Info("process-path catalogue consumer running", "topic", kafkacatalog.Topic)
			if err := kafkaCatalogue.Run(catalogueConsumerCtx); err != nil {
				logger.Error("process-path catalogue consumer stopped", "error", err)
			}
		}()

		logger.Info("waiting for the process-path catalogue to replay its initial history before accepting traffic")
		waitCtx, waitCancel := context.WithTimeout(context.Background(), kafkacatalog.WaitReadyTimeout)
		err = kafkaCatalogue.WaitReady(waitCtx)
		waitCancel()
		if err != nil {
			return fmt.Errorf("process-path catalogue did not become ready within %s: %w", kafkacatalog.WaitReadyTimeout, err)
		}
		logger.Info("process-path catalogue is ready", "paths", kafkaCatalogue.Ids())
		catalogue = kafkaCatalogue
	default:
		// The process-path catalogue is loaded and validated once at
		// boot, before anything else stands up — a missing or
		// malformed catalogue file must stop this service from
		// starting at all, never fall back to a partial/empty
		// catalogue (mirrors fulfillment-execution's identical
		// boot-time contract; see ADR-0017 there and this service's
		// own ADR-0012).
		fileCatalogue, err := filecatalog.Load(getenv("PATH_CATALOGUE_FILE", "/etc/wes-work-planning/process-paths.yaml"))
		if err != nil {
			return fmt.Errorf("failed to load the process-path catalogue: %w", err)
		}
		logger.Info("process-path catalogue loaded", "paths", fileCatalogue.Ids())
		catalogue = fileCatalogue
	}

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
		// The OLTP schema is a precondition this process enforces itself,
		// rather than assuming an out-of-band golang-migrate CLI step ran.
		// That assumption silently did not hold: the service deployed
		// cleanly against an empty database and every Postgres-backed
		// endpoint failed at request time with
		// `relation "charge_forecasts" does not exist`. Migrating before
		// the pool is opened matches what fulfillment-execution's and
		// workforce-management's OLTP binaries already do.
		if err := postgres.Migrate(databaseURL, migrationsPath); err != nil {
			return err
		}

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
		brokers := brokerList(kafkaBrokers)
		// The integration publisher (warehouse.work-planning.events) is
		// untouched. Alongside it, a SEPARATE analytics publisher fans every
		// domain event onto the dedicated analytics topic
		// (warehouse.wes.analytics) that feeds the "Release Throughput &
		// Backlog Health" data product. A MultiPublisher emits each event to
		// BOTH, exactly once each, without either publisher knowing about the
		// other (ADR-0011).
		integrationPublisher := outboundkafka.NewPublisher(brokers, workUnits, classifications, newEventID)
		defer func() { _ = integrationPublisher.Close() }()
		analyticsPublisher := outboundkafka.NewAnalyticsPublisher(brokers, newEventID)
		defer func() { _ = analyticsPublisher.Close() }()
		publisher = events.NewMultiPublisher(integrationPublisher, analyticsPublisher)
	default:
		publisher = events.NewLogPublisher(logger)
	}

	recordCompletion := usecases.NewRecordCompletion(workUnits, pools, publisher, clock)
	enqueueWorkUnit := usecases.NewEnqueueWorkUnit(workUnits, pools, publisher, clock)

	handlers := &inboundhttp.Handlers{
		ReceiveChargeForecast:   usecases.NewReceiveChargeForecast(charges, publisher, clock),
		CommitShiftPlan:         usecases.NewCommitShiftPlan(plans, publisher, clock),
		EnqueueWorkUnit:         enqueueWorkUnit,
		ReleaseNextWork:         usecases.NewReleaseNextWork(pools, workUnits, publisher, clock),
		RecordCompletion:        recordCompletion,
		SampleBacklog:           usecases.NewSampleBacklog(pools, publisher, clock),
		RebalanceDecision:       usecases.NewRebalanceDecision(pools, publisher, clock),
		Catalogue:               catalogue,
		LaborPlanView:           usecases.NewLaborPlanView(laborPlanViews),
		InventoryView:           usecases.NewInventoryView(inventoryViews),
		GetWorkUnitsByReference: usecases.NewGetWorkUnitsByReference(workUnits),
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
		consumer = inboundkafka.NewConsumer(brokerList(kafkaBrokers), "wes-work-planning", observeLabor, observeInventory, recordCompletion, enqueueWorkUnit, processedEvts, catalogue, logger)
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
		cancelCatalogueConsumer()
		if kafkaCatalogue != nil {
			_ = kafkaCatalogue.Close()
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
