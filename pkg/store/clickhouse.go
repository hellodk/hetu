package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"

	"github.com/hellodk/hetu/pkg/config"
)

// OpenClickHouse opens a *sql.DB against the configured ClickHouse endpoint
// and verifies connectivity with a ping.
func OpenClickHouse(ctx context.Context, cfg config.ClickHouseConfig) (*sql.DB, error) {
	if !cfg.Enabled {
		return nil, errors.New("store: clickhouse is not enabled")
	}

	if cfg.DSN != "" {
		db := clickhouse.OpenDB(&clickhouse.Options{
			Addr: cfg.Hosts,
		})
		pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := db.PingContext(pingCtx); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("store: ping clickhouse (dsn): %w", err)
		}
		return db, nil
	}

	if len(cfg.Hosts) == 0 {
		return nil, errors.New("store: clickhouse hosts required")
	}

	addrs := make([]string, len(cfg.Hosts))
	for i, h := range cfg.Hosts {
		addrs[i] = fmt.Sprintf("%s:%d", h, cfg.Port)
	}

	db := clickhouse.OpenDB(&clickhouse.Options{
		Addr: addrs,
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.User,
			Password: cfg.Password,
		},
		DialTimeout: cfg.DialTimeout,
		TLS:         nil, // TODO: TLS config if cfg.Secure
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		MaxOpenConns: cfg.MaxOpenConns,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping clickhouse: %w", err)
	}
	return db, nil
}
