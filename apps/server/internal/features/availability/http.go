package availability

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/lots"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// Handler serves the public availability and spaces endpoints.
type Handler struct {
	svc  *Service
	lots lots.Resolver
	log  *slog.Logger
}

// NewHandler returns a Handler.
func NewHandler(svc *Service, lots lots.Resolver, log *slog.Logger) *Handler {
	return &Handler{svc: svc, lots: lots, log: log}
}

// Register adds the availability routes to r.
func (h *Handler) Register(r *router.Router) {
	p := router.Policy{Public: true, Limit: &ratelimit.PublicReadIP}
	r.Handle("GET /v1/lots/{id}/availability", p, h.availability)
	r.Handle("GET /v1/lots/{id}/spaces", p, h.spaces)
}

func (h *Handler) lotID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return resolveLot(w, r, h.lots, h.log)
}

// resolveLot resolves the path id; it writes the 404 and returns false when
// the id is malformed or unknown (a lot that does not exist, never a 400/422).
func resolveLot(w http.ResponseWriter, r *http.Request, res lots.Resolver, log *slog.Logger) (int64, bool) {
	publicID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, r, log, apperr.NotFound(errcodes.LotNotFound))
		return 0, false
	}
	id, err := res.LotIDByPublicID(r.Context(), publicID)
	if errors.Is(err, lots.ErrNotFound) {
		httpx.WriteError(w, r, log, apperr.NotFound(errcodes.LotNotFound))
		return 0, false
	}
	if err != nil {
		httpx.WriteError(w, r, log, err)
		return 0, false
	}
	return id, true
}

func (h *Handler) availability(w http.ResponseWriter, r *http.Request) {
	id, ok := h.lotID(w, r)
	if !ok {
		return
	}
	snap, err := h.svc.Snapshot(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusOK, snap)
}

func (h *Handler) spaces(w http.ResponseWriter, r *http.Request) {
	id, ok := h.lotID(w, r)
	if !ok {
		return
	}
	spaces, err := h.svc.Spaces(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if spaces == nil {
		spaces = []SpaceState{}
	}
	response.OK(w, r, http.StatusOK, struct {
		Spaces []SpaceState `json:"spaces"`
	}{spaces})
}
