// Package usecases implements the application's use cases: one struct per
// use case, orchestrating the domain against outbound ports.
package usecases

import (
	"context"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/charge"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// ReceiveChargeForecast records the volume due by CPT for a process path.
type ReceiveChargeForecast struct {
	charges   ports.ChargeRepo
	publisher ports.EventPublisher
	clock     ports.Clock
	uow       ports.UnitOfWork
}

func NewReceiveChargeForecast(charges ports.ChargeRepo, publisher ports.EventPublisher, clock ports.Clock) *ReceiveChargeForecast {
	return &ReceiveChargeForecast{charges: charges, publisher: publisher, clock: clock}
}

// WithUnitOfWork brackets Save + Publish in one atomic scope (ADR-0014).
// Optional: nil keeps the two calls running back to back.
func (uc *ReceiveChargeForecast) WithUnitOfWork(u ports.UnitOfWork) *ReceiveChargeForecast {
	uc.uow = u
	return uc
}

// CPTBucketInput is one CPT/quantity pair in the incoming forecast.
type CPTBucketInput struct {
	CPT      shared.CPT
	Quantity shared.Quantity
}

type ReceiveChargeForecastRequest struct {
	PathId  shared.PathId
	Buckets []CPTBucketInput
}

func (uc *ReceiveChargeForecast) Execute(ctx context.Context, req ReceiveChargeForecastRequest) (*charge.ChargeForecast, error) {
	buckets := make([]charge.CPTBucket, len(req.Buckets))
	for i, b := range req.Buckets {
		buckets[i] = charge.CPTBucket{CPT: b.CPT, Quantity: b.Quantity}
	}

	forecast, err := charge.NewChargeForecast(req.PathId, buckets, uc.clock.Now())
	if err != nil {
		return nil, err
	}

	event := shared.NewChargeForecastReceived(req.PathId, uc.clock.Now())
	err = atomically(ctx, uc.uow, func(ctx context.Context) error {
		if err := uc.charges.Save(ctx, forecast); err != nil {
			return err
		}
		return uc.publisher.Publish(ctx, event)
	})
	if err != nil {
		return nil, err
	}

	return forecast, nil
}
