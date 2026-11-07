package legal

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// Handler serves the legal endpoints.
type Handler struct {
	current Current
	log     *slog.Logger
}

// NewHandler returns a Handler over current.
func NewHandler(current Current, log *slog.Logger) *Handler {
	return &Handler{current: current, log: log}
}

// Register adds the legal routes to r.
func (h *Handler) Register(r *router.Router) {
	r.Handle("GET /v1/legal/current", router.Policy{Public: true}, h.getCurrent)
}

type documentDTO struct {
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"published_at"`
	URLEs       string    `json:"url_es"`
	URLEn       string    `json:"url_en"`
}

type currentDTO struct {
	Terms   documentDTO `json:"terms"`
	Privacy documentDTO `json:"privacy"`
}

func toDTO(d Document) documentDTO {
	return documentDTO{Version: d.Version, PublishedAt: d.PublishedAt.UTC(), URLEs: d.URLEs, URLEn: d.URLEn}
}

func (h *Handler) getCurrent(w http.ResponseWriter, r *http.Request) {
	v, err := h.current.Versions(r.Context())
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusOK, currentDTO{Terms: toDTO(v.Terms), Privacy: toDTO(v.Privacy)})
}
