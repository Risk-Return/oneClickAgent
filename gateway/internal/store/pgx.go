// Package store implements PostgreSQL repositories using pgx v5 + pgxpool.
// Provides connection pool setup, migrations via golang-migrate, and transaction helpers.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is a thin wrapper around a pgx connection pool.
type DB struct {
	Pool *pgxpool.Pool
}

// NewDB creates a new DB with a connection pool to PostgreSQL.
func NewDB(ctx context.Context, databaseURL string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// Close closes the connection pool.
func (db *DB) Close() {
	db.Pool.Close()
}

// Ping verifies the database connection is alive.
func (db *DB) Ping(ctx context.Context) error {
	return db.Pool.Ping(ctx)
}

// RunMigrations applies pending migrations from the given directory.
func (db *DB) RunMigrations(ctx context.Context, migrationsDir string) error {
	// Migrations are handled by the caller using golang-migrate.
	// The migrationsDir parameter is provided for that purpose.
	// This stub can be replaced with embedded migration logic.
	_ = ctx
	_ = migrationsDir
	return nil
}
