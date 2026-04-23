package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file" // file:// source

	"github.com/hellodk/hetu/pkg/config"
)

// MigratePostgres applies all up-migrations from cfg.MigrationsPath against
// the supplied database handle. Idempotent: returns nil when there is nothing
// to apply. The function uses a Postgres advisory lock under the hood
// (golang-migrate's default) so concurrent analyzer replicas race safely.
func MigratePostgres(db *sql.DB, cfg config.PostgresConfig) error {
	if cfg.MigrationsPath == "" {
		return errors.New("store: migrations path is empty")
	}

	driver, err := postgres.WithInstance(db, &postgres.Config{
		DatabaseName: cfg.Database,
	})
	if err != nil {
		return fmt.Errorf("store: migrate driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://"+cfg.MigrationsPath,
		cfg.Database,
		driver,
	)
	if err != nil {
		return fmt.Errorf("store: migrate init: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("store: migrate up: %w", err)
	}
	return nil
}
