package tariffs

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/lots"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// Handler serves the public tariff endpoint.
type Handler struct {
	cur  Current
	lots lots.Resolver
	clk  clock.Clock
	log  *slog.Logger
}

// NewHandler returns a Handler. clk decides which version is in force.
func NewHandler(cur Current, lots lots.Resolver, clk clock.Clock, log *slog.Logger) *Handler {
	return &Handler{cur: cur, lots: lots, clk: clk, log: log}
}

// Register adds the tariff route to r.
func (h *Handler) Register(r *router.Router) {
	r.Handle("GET /v1/lots/{id}/tariff", router.Policy{Public: true, Limit: &ratelimit.PublicReadIP}, h.get)
}

// tariffDTO is the design §11.1 Tariff.
type tariffDTO struct {
	EffectiveFrom          time.Time `json:"effective_from"`
	Currency               string    `json:"currency"`
	FirstHourCents         int64     `json:"first_hour_cents"`
	AdditionalHourCents    int64     `json:"additional_hour_cents"`
	ExitGraceMinutes       int64     `json:"exit_grace_minutes"`
	PostPaymentExitMinutes int64     `json:"post_payment_exit_minutes"`
	DailyCapCents          int64     `json:"daily_cap_cents"`
	LostTicketCents        int64     `json:"lost_ticket_cents"`
	ReservationFeeCents    int64     `json:"reservation_fee_cents"`
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	// A malformed id is a lot that does not exist, never a 400/422.
	publicID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, r, h.log, apperr.NotFound(errcodes.LotNotFound))
		return
	}
	lotID, err := h.lots.LotIDByPublicID(r.Context(), publicID)
	if errors.Is(err, lots.ErrNotFound) {
		httpx.WriteError(w, r, h.log, apperr.NotFound(errcodes.LotNotFound))
		return
	}
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.cur.AtTime(r.Context(), lotID, h.clk.Now())
	if err != nil {
		// A lot without a tariff in force is a seeding fault: 500, logged.
		httpx.WriteError(w, r, h.log, err)
		return
	}
	t := v.Tariff
	response.OK(w, r, http.StatusOK, tariffDTO{
		EffectiveFrom:          t.EffectiveFrom.UTC(),
		Currency:               "DOP",
		FirstHourCents:         t.FirstHour,
		AdditionalHourCents:    t.AdditionalHour,
		ExitGraceMinutes:       int64(t.ExitGrace / time.Minute),
		PostPaymentExitMinutes: int64(t.PostPaymentExit / time.Minute),
		DailyCapCents:          t.DailyCap,
		LostTicketCents:        t.LostTicket,
		ReservationFeeCents:    t.ReservationFee,
	})
}
