package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/your-org/cluster-intel/pkg/config"
)

// OpenRedis opens a Redis client using the supplied configuration and verifies
// connectivity with a ping. Supports both standalone and Sentinel topologies.
func OpenRedis(ctx context.Context, cfg config.RedisConfig) (*redis.Client, error) {
	if !cfg.Enabled {
		return nil, errors.New("store: redis is not enabled")
	}

	var rdb *redis.Client
	if cfg.Sentinel != nil && cfg.Sentinel.MasterName != "" {
		rdb = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    cfg.Sentinel.MasterName,
			SentinelAddrs: cfg.Sentinel.Addrs,
			Username:      cfg.Username,
			Password:      cfg.Password,
			DB:            cfg.DB,
			DialTimeout:   cfg.DialTimeout,
			PoolSize:      cfg.PoolSize,
		})
	} else {
		rdb = redis.NewClient(&redis.Options{
			Addr:        cfg.Addr,
			Username:    cfg.Username,
			Password:    cfg.Password,
			DB:          cfg.DB,
			DialTimeout: cfg.DialTimeout,
			PoolSize:    cfg.PoolSize,
		})
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("store: ping redis: %w", err)
	}
	return rdb, nil
}
