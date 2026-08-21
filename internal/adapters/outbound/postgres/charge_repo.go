// Package postgres provides pgxpool-backed implementations of every
// outbound port.
package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/charge"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

type ChargeRepo struct {
	pool *pgxpool.Pool
}

func NewChargeRepo(pool *pgxpool.Pool) *ChargeRepo {
	return &ChargeRepo{pool: pool}
}

type bucketDTO struct {
	CPTUnixNano int64 `json:"cpt_unix_nano"`
	Quantity    int   `json:"quantity"`
}

func (r *ChargeRepo) Save(ctx context.Context, forecast *charge.ChargeForecast) error {
	buckets := forecast.Buckets()
	dtos := make([]bucketDTO, len(buckets))
	for i, b := range buckets {
		dtos[i] = bucketDTO{CPTUnixNano: b.CPT.Time().UnixNano(), Quantity: b.Quantity.Value()}
	}
	payload, err := json.Marshal(dtos)
	if err != nil {
		return err
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO charge_forecasts (path_id, received_at, buckets)
		VALUES ($1, $2, $3)
		ON CONFLICT (path_id) DO UPDATE SET received_at = $2, buckets = $3
	`, forecast.PathId().String(), forecast.ReceivedAt(), payload)
	return err
}

func (r *ChargeRepo) FindByPathId(ctx context.Context, pathId shared.PathId) (*charge.ChargeForecast, error) {
	var receivedAt = forecastRow{}
	row := r.pool.QueryRow(ctx, `SELECT received_at, buckets FROM charge_forecasts WHERE path_id = $1`, pathId.String())
	if err := row.Scan(&receivedAt.ReceivedAt, &receivedAt.Buckets); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ports.ErrNotFound
		}
		return nil, err
	}

	var dtos []bucketDTO
	if err := json.Unmarshal(receivedAt.Buckets, &dtos); err != nil {
		return nil, err
	}

	cptBuckets := make([]charge.CPTBucket, len(dtos))
	for i, d := range dtos {
		qty, err := shared.NewQuantity(d.Quantity)
		if err != nil {
			return nil, err
		}
		cptBuckets[i] = charge.CPTBucket{CPT: shared.NewCPT(unixNanoToTime(d.CPTUnixNano)), Quantity: qty}
	}

	return charge.NewChargeForecast(pathId, cptBuckets, receivedAt.ReceivedAt)
}
