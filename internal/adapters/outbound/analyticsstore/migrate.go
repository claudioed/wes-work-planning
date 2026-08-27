package analyticsstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrate applies every *.up.sql file under migrationsPath, in ascending
// filename order, to the analytical database at databaseURL. It records
// applied filenames in a schema_migrations_analytics table and skips any
// already applied, so it is safe to run on every projector start (the
// projector owns the analytical schema — ADR-0011).
//
// This is a deliberately small, dependency-free runner: the analytical
// migration set is a single additive forward-only schema, and the OLTP side
// already applies its own migrations with the external golang-migrate CLI, so
// pulling that library in only for the projector is not worth the added
// supply-chain surface.
func Migrate(databaseURL, migrationsPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("analyticsstore: migrate connect: %w", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations_analytics (
			filename   TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("analyticsstore: migrate bookkeeping table: %w", err)
	}

	files, err := upMigrationFiles(migrationsPath)
	if err != nil {
		return err
	}

	for _, path := range files {
		name := filepath.Base(path)

		var exists bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations_analytics WHERE filename = $1)`,
			name).Scan(&exists); err != nil {
			return fmt.Errorf("analyticsstore: migrate check %s: %w", name, err)
		}
		if exists {
			continue
		}

		sqlBytes, err := os.ReadFile(path) //nolint:gosec // migrationsPath is operator-controlled config, not user input.
		if err != nil {
			return fmt.Errorf("analyticsstore: read migration %s: %w", name, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("analyticsstore: migrate begin %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("analyticsstore: apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations_analytics (filename) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("analyticsstore: record migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("analyticsstore: commit migration %s: %w", name, err)
		}
	}
	return nil
}

// upMigrationFiles returns the absolute paths of every *.up.sql file directly
// under dir, sorted ascending by filename so 0001 applies before 0002.
func upMigrationFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("analyticsstore: read migrations dir %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}
