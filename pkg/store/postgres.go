package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx as database/sql driver

	"github.com/hellodk/hetu/pkg/config"
)

// OpenPostgres opens a *sql.DB against the configured Postgres endpoint and
// verifies connectivity with a ping. The DSN is built from the structured
// fields unless cfg.DSN is non-empty, in which case it is used verbatim.
//
// Connection-pool tuning fields (MaxOpenConns, MaxIdleConns, ConnMaxLifetime)
// are applied before returning. Caller is responsible for Close().
func OpenPostgres(ctx context.Context, cfg config.PostgresConfig) (*sql.DB, error) {
	if !cfg.Enabled {
		return nil, errors.New("store: postgres is not enabled")
	}

	dsn := cfg.DSN
	if dsn == "" {
		dsn = buildPostgresDSN(cfg)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open postgres: %w", err)
	}

	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping postgres: %w", err)
	}
	return db, nil
}

// buildPostgresDSN renders a libpq-style URL DSN from the structured fields.
// Format: postgres://user:password@host:port/database?sslmode=...&application_name=...
func buildPostgresDSN(cfg config.PostgresConfig) string {
	u := &url.URL{
		Scheme: "postgres",
		Host:   fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Path:   "/" + cfg.Database,
	}
	if cfg.User != "" {
		if cfg.Password != "" {
			u.User = url.UserPassword(cfg.User, cfg.Password)
		} else {
			u.User = url.User(cfg.User)
		}
	}
	q := u.Query()
	if cfg.SSLMode != "" {
		q.Set("sslmode", cfg.SSLMode)
	}
	if cfg.SSLRootCertFile != "" {
		q.Set("sslrootcert", cfg.SSLRootCertFile)
	}
	if cfg.AppName != "" {
		q.Set("application_name", cfg.AppName)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
