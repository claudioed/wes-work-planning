// Command wes is the composition root: it wires config from env into
// adapters, use cases, and the HTTP router, then serves.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	inboundhttp "github.com/claudioed/wes-work-planning/internal/adapters/inbound/http"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/events"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/memory"
	"github.com/claudioed/wes-work-planning/internal/adapters/outbound/postgres"
	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/application/usecases"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	httpAddr := getenv("HTTP_ADDR", ":8080")
	databaseURL := os.Getenv("DATABASE_URL")

	logger := log.New(os.Stdout, "wes ", log.LstdFlags)
	publisher := events.NewLogPublisher(logger)
	clock := memory.SystemClock{}

	var (
		charges   ports.ChargeRepo
		plans     ports.PlanRepo
		pools     ports.WorkPoolRepo
		workUnits ports.WorkUnitRepo
	)

	if databaseURL == "" {
		logger.Println("DATABASE_URL not set; using in-memory adapters")
		charges = memory.NewChargeRepo()
		plans = memory.NewPlanRepo()
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

		charges = postgres.NewChargeRepo(pool)
		plans = postgres.NewPlanRepo(pool)
		pools = postgres.NewWorkPoolRepo(pool)
		workUnits = postgres.NewWorkUnitRepo(pool)
	}

	handlers := &inboundhttp.Handlers{
		ReceiveChargeForecast: usecases.NewReceiveChargeForecast(charges, publisher, clock),
		CommitShiftPlan:       usecases.NewCommitShiftPlan(plans, publisher, clock),
		EnqueueWorkUnit:       usecases.NewEnqueueWorkUnit(workUnits, pools, publisher, clock),
		ReleaseNextWork:       usecases.NewReleaseNextWork(pools, workUnits, publisher, clock),
		RecordCompletion:      usecases.NewRecordCompletion(workUnits, publisher, clock),
		SampleBacklog:         usecases.NewSampleBacklog(pools, publisher, clock),
		RebalanceDecision:     usecases.NewRebalanceDecision(pools, publisher, clock),
	}

	router := inboundhttp.NewRouter(handlers)

	server := &http.Server{
		Addr:              httpAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Printf("listening on %s", httpAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case <-sigCh:
		logger.Println("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
