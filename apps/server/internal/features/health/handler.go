package health

import (
	"context"
	"net/http"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
)

// Check results, as defined by ReadinessChecks in api/openapi.yaml.
const (
	checkOK     = "ok"
	checkFail   = "fail"
	checkBehind = "behind"
)

// readyTimeout bounds the dependency checks of one /readyz request.
const readyTimeout = 2 * time.Second

// Handler serves the health endpoints.
type Handler struct {
	service    string
	version    string
	db         Pinger
	migrations MigrationStatus
}

// NewHandler returns a Handler for the given service name and build version.
func NewHandler(service, version string, p Pinger, m MigrationStatus) *Handler {
	return &Handler{service: service, version: version, db: p, migrations: m}
}

// Register adds GET /healthz and GET /readyz to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)
}

type healthBody struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

type readinessChecks struct {
	Database   string `json:"database"`
	Migrations string `json:"migrations"`
}

type readinessBody struct {
	Status string          `json:"status"`
	Checks readinessChecks `json:"checks"`
}

// healthz is the liveness probe: it never checks dependencies.
func (h *Handler) healthz(w http.ResponseWriter, _ *http.Request) {
	response.JSON(w, http.StatusOK, healthBody{Status: "ok", Service: h.service, Version: h.version})
}

// readyz returns 200 only when the database answers and migrations are
// current; otherwise 503 health.not_ready with both checks in details.
func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()

	checks := h.check(ctx)
	if checks.Database == checkOK && checks.Migrations == checkOK {
		response.JSON(w, http.StatusOK, readinessBody{Status: "ready", Checks: checks})
		return
	}
	response.Error(w, r, http.StatusServiceUnavailable, errcodes.HealthNotReady, "service is not ready",
		map[string]any{"database": checks.Database, "migrations": checks.Migrations})
}

// check runs the readiness checks. When the database is unreachable the
// migration status cannot be read either, so Status is not called and
// migrations are reported as "fail". A schema ahead of the binary
// (current > expected, e.g. during a rolling deploy) counts as "ok".
func (h *Handler) check(ctx context.Context) readinessChecks {
	if err := h.db.Ping(ctx); err != nil {
		return readinessChecks{Database: checkFail, Migrations: checkFail}
	}
	current, expected, err := h.migrations.Status(ctx)
	switch {
	case err != nil:
		return readinessChecks{Database: checkOK, Migrations: checkFail}
	case current < expected:
		return readinessChecks{Database: checkOK, Migrations: checkBehind}
	default:
		return readinessChecks{Database: checkOK, Migrations: checkOK}
	}
}
