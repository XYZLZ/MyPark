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
	extra      []namedCheck
}

// namedCheck is a readiness check added with WithCheck.
type namedCheck struct {
	name string
	fn   func(ctx context.Context) bool
}

// NewHandler returns a Handler for the given service name and build version.
func NewHandler(service, version string, p Pinger, m MigrationStatus) *Handler {
	return &Handler{service: service, version: version, db: p, migrations: m}
}

// WithCheck adds a readiness check reported under name as "ok" or "fail"; a
// failing check makes /readyz answer 503 health.not_ready. Adding a name again
// replaces its check. It returns h. Call it before serving.
func (h *Handler) WithCheck(name string, fn func(ctx context.Context) bool) *Handler {
	for i, c := range h.extra {
		if c.name == name {
			h.extra[i].fn = fn
			return h
		}
	}
	h.extra = append(h.extra, namedCheck{name: name, fn: fn})
	return h
}

// Register adds GET /healthz and GET /readyz to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	RegisterLiveness(mux, h.service, h.version)
	mux.HandleFunc("GET /readyz", h.readyz)
}

// RegisterLiveness adds only GET /healthz to mux, for a service with no
// dependencies to check (cmd/simulator). It needs no Pinger or
// MigrationStatus, so there is no /readyz to call them.
func RegisterLiveness(mux *http.ServeMux, service, version string) {
	body := healthBody{Status: "ok", Service: service, Version: version}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		response.OK(w, r, http.StatusOK, body)
	})
}

// healthBody is the /healthz answer. Liveness never checks dependencies.
type healthBody struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

type readinessChecks struct {
	Database   string
	Migrations string
}

type readinessBody struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// readyz returns 200 only when the database answers, migrations are current
// and every added check passes; otherwise 503 health.not_ready with every
// check in details.
func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()

	base := h.check(ctx)
	checks := map[string]string{"database": base.Database, "migrations": base.Migrations}
	ready := base.Database == checkOK && base.Migrations == checkOK
	for _, c := range h.extra {
		checks[c.name] = checkFail
		if c.fn(ctx) {
			checks[c.name] = checkOK
		} else {
			ready = false
		}
	}
	if ready {
		response.OK(w, r, http.StatusOK, readinessBody{Status: "ready", Checks: checks})
		return
	}
	details := make(map[string]any, len(checks))
	for k, v := range checks {
		details[k] = v
	}
	response.Error(w, r, http.StatusServiceUnavailable, errcodes.HealthNotReady, "service is not ready", details)
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
