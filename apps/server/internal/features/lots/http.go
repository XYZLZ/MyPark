package lots

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// StatusSource gives a lot's public status (open, full or closed), which the
// availability feature owns.
type StatusSource interface {
	LotStatus(ctx context.Context, lotID int64) (string, error)
}

// pageLimit is the nominal page size; M2 has one lot, so it never pages.
const pageLimit = 50

// Handler serves the public lot catalogue.
type Handler struct {
	svc    *Service
	status StatusSource
	log    *slog.Logger
}

// NewHandler returns a Handler.
func NewHandler(svc *Service, status StatusSource, log *slog.Logger) *Handler {
	return &Handler{svc: svc, status: status, log: log}
}

// Register adds the lot routes to r.
func (h *Handler) Register(r *router.Router) {
	p := router.Policy{Public: true, Limit: &ratelimit.PublicReadIP}
	r.Handle("GET /v1/lots", p, h.list)
	r.Handle("GET /v1/lots/{id}", p, h.get)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.List(r.Context())
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	// M2 has one lot: the page never has a next cursor.
	response.List(w, r, items, response.Page{Limit: pageLimit})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	// A malformed id is a lot that does not exist, never a 400/422.
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, r, h.log, apperr.NotFound(errcodes.LotNotFound))
		return
	}
	lot, err := h.svc.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpx.WriteError(w, r, h.log, apperr.NotFound(errcodes.LotNotFound))
		return
	}
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if lot.Status, err = h.status.LotStatus(r.Context(), lot.InternalID); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusOK, lot)
}
