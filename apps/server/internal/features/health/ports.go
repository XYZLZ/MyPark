// Package health serves the liveness (/healthz) and readiness (/readyz)
// probes described in api/openapi.yaml.
package health

import "context"

// Pinger checks that the database is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// MigrationStatus reports the applied schema version (current) and the
// version this binary expects (expected).
type MigrationStatus interface {
	Status(ctx context.Context) (current, expected int64, err error)
}
