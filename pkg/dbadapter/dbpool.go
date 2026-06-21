package dbadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DBPoolManager struct {
	pool *pgxpool.Pool
	cfg  Config
	logf func(string, ...any)
}

func NewDBPoolManager(ctx context.Context, cfg Config, logf func(string, ...any)) (*DBPoolManager, error) {

	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("dbadapter: parse DSN: %w", err)
	}

	poolCfg.MaxConns = int32(cfg.MaxOpenConns)
	poolCfg.MinConns = int32(cfg.MaxIdleConns)
	poolCfg.MaxConnLifetime = cfg.ConnMaxLifetime
	poolCfg.MaxConnIdleTime = cfg.ConnMaxLifetime / 2

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("dbadapter: create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("dbadapter: ping: %w", err)
	}

	logf("dbadapter: pool created (max_open=%d max_idle=%d max_lifetime=%v)",
		cfg.MaxOpenConns, cfg.MaxIdleConns, cfg.ConnMaxLifetime)

	return &DBPoolManager{
		pool: pool,
		cfg:  cfg,
		logf: logf,
	}, nil
}

func (m *DBPoolManager) Pool() *pgxpool.Pool {
	return m.pool
}

func (m *DBPoolManager) ActiveConnections() int {
	stat := m.pool.Stat()
	return int(stat.AcquiredConns())
}

func (m *DBPoolManager) Close() {
	m.pool.Close()
	m.logf("dbadapter: pool closed")
}
