// Command mcp is the composition root for the wes-work-planning MCP server:
// it wires env config to outbound adapters, adapters to the use cases, and
// those to the inbound MCP adapter, then serves MCP over Streamable HTTP. It
// is a second, independent deployable alongside cmd/wes (the HTTP service),
// per ADR-0008.
//
// Auth is a static bearer key (no IdP): set MCP_READ_KEY (and optionally
// MCP_READWRITE_KEY) from a Kubernetes Secret. A request must present a valid
// key; the scope it grants gates the tools.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	inboundmcp "github.com/claudioed/wes-work-planning/internal/adapters/inbound/mcp"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/events"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/memory"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/postgres"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/telemetry"
	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/application/usecases"
)

// serviceName is this server's identity in OTel resource attributes and
// span/metric scopes; OTEL_SERVICE_NAME can override it.
const serviceName = "wes-work-planning-mcp"

func main() {
	if err := run(); err != nil {
		slog.Error("mcp server exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	logger := newLogger(getenv("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	// Same non-blocking telemetry setup as the HTTP service: the exporters
	// dial lazily, so an unreachable Collector degrades to dropped telemetry,
	// never a server that won't start.
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
		if err := shutdownTelemetry(ctx); err != nil {
			logger.Warn("telemetry shutdown did not flush cleanly", "error", err)
		}
	}()

	httpAddr := getenv("MCP_ADDR", ":8090")
	databaseURL := os.Getenv("DATABASE_URL")

	clock := memory.SystemClock{}

	var (
		pools     ports.WorkPoolRepo
		workUnits ports.WorkUnitRepo
	)

	// Select in-memory vs Postgres exactly as cmd/wes does. The MCP adapter
	// only needs the work-pool and work-unit repositories for its read/write
	// use cases.
	if databaseURL == "" {
		logger.Info("database url not configured; using in-memory adapters")
		pools = memory.NewWorkPoolRepo()
		workUnits = memory.NewWorkUnitRepo()
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		pool, err := postgres.Connect(ctx, databaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()

		pools = postgres.NewWorkPoolRepo(pool)
		workUnits = postgres.NewWorkUnitRepo(pool)
	}

	// The MCP adapter reuses the SAME use cases the HTTP adapter uses:
	// SampleBacklog and RebalanceDecision (read) and ReleaseNextWork (write).
	// Those use cases need a publisher and clock; the MCP server is not the
	// platform's primary event publisher (cmd/wes is), so it logs any event it
	// raises rather than publishing to Kafka.
	publisher := events.NewLogPublisher(logger)
	deps := inboundmcp.Deps{
		SampleBacklog:     usecases.NewSampleBacklog(pools, publisher, clock),
		RebalanceDecision: usecases.NewRebalanceDecision(pools, publisher, clock),
		ReleaseNextWork:   usecases.NewReleaseNextWork(pools, workUnits, publisher, clock),
	}
	server := inboundmcp.NewServer(deps)

	auth := inboundmcp.NewStaticKeyAuth(authKeys(logger))
	handler := inboundmcp.Handler(server, auth)

	srv := &http.Server{
		Addr:              httpAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("mcp server listening (Streamable HTTP)", "addr", httpAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case <-sigCh:
		logger.Info("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

// authKeys reads the bearer keys from the environment. MCP_READ_KEY grants
// read scope; MCP_READWRITE_KEY grants read-write. If neither is set the server
// still starts but rejects every request (fail closed) — a missing key must
// never mean "open to everyone". The keys themselves are never logged.
func authKeys(logger *slog.Logger) map[string]inboundmcp.Scope {
	keys := make(map[string]inboundmcp.Scope)
	if k := os.Getenv("MCP_READ_KEY"); k != "" {
		keys[k] = inboundmcp.ScopeRead
	}
	if k := os.Getenv("MCP_READWRITE_KEY"); k != "" {
		keys[k] = inboundmcp.ScopeReadWrite
	}
	if len(keys) == 0 {
		logger.Warn("no MCP_READ_KEY or MCP_READWRITE_KEY set; server will reject all requests")
	}
	return keys
}

// newLogger builds the process-wide structured logger: JSON to stdout, at the
// level LOG_LEVEL names (debug|info|warn|error, case-insensitive, default
// info). Records are routed through telemetry.NewTraceHandler so any log
// emitted inside a span carries that span's trace_id and span_id.
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

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
