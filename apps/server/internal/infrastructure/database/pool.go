// Package database is the Postgres layer: the pgx connection pool, the
// embedded goose migrations and the sqlc-generated queries (package dbgen).
package database

import (
	"cmp"
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Runtime parameters applied to every pooled connection unless the URL
// already sets them.
const (
	defaultStatementTimeout = "5s"
	defaultApplicationName  = "mypark"
)

// NewPool returns a pgx pool for url, capped at maxConns connections. It sets
// statement_timeout=5s and application_name=mypark on every connection
// (values given in the URL win). The pool connects lazily: an unreachable
// database is reported by Ping, not here, so the API can boot and report
// 503 on /readyz instead of crash-looping.
func NewPool(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		// pgx redacts the password in parse errors.
		return nil, fmt.Errorf("database: parse connection URL: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	params := cfg.ConnConfig.RuntimeParams
	params["statement_timeout"] = cmp.Or(params["statement_timeout"], defaultStatementTimeout)
	params["application_name"] = cmp.Or(params["application_name"], defaultApplicationName)

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: create pool: %w", err)
	}
	return pool, nil
}
