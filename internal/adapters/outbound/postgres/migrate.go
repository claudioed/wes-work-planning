package postgres

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	// Register the postgres database driver and the file source driver used
	// by Migrate below. Both are imported for their side effects only.
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// Migrate applies every pending migration under migrationsPath to the OLTP
// database at databaseURL, and is safe to call on every start: a database
// already at the latest version yields migrate.ErrNoChange, which is not an
// error here.
//
// This runs from the service's own composition root rather than from an
// out-of-band CLI step. The alternative — expecting an operator (or a
// deployment's init container / Job) to run the golang-migrate CLI — is what
// this repo previously assumed, and it silently did not happen: the service
// deployed cleanly against a completely empty database and every
// Postgres-backed endpoint failed at request time with
// `relation "charge_forecasts" does not exist`. Migrating at startup makes
// the schema a precondition the process itself enforces, matching what
// fulfillment-execution's and workforce-management's OLTP binaries already
// do.
//
// Note this is the OLTP schema only. The analytical schema is owned by
// cmd/wes-projector and applied by analyticsstore.Migrate (ADR-0011) — the
// two are deliberately separate databases with separate migration sets, and
// neither runner should ever touch the other's.
func Migrate(databaseURL, migrationsPath string) error {
	m, err := migrate.New(fmt.Sprintf("file://%s", migrationsPath), databaseURL)
	if err != nil {
		return fmt.Errorf("postgres: open migrations at %s: %w", migrationsPath, err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("postgres: apply migrations: %w", err)
	}
	return nil
}
