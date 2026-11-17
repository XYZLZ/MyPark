package reservations

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// Handler serves the reservation endpoints.
type Handler struct {
	svc *Service
	v   *validation.Validator
	log *slog.Logger
}

// NewHandler returns a Handler.
func NewHandler(svc *Service, v *validation.Validator, log *slog.Logger) *Handler {
	return &Handler{svc: svc, v: v, log: log}
}

// Register adds the reservation routes to r. All of them are driver-only and
// sit behind the legal gate.
func (h *Handler) Register(r *router.Router) {
	r.Handle("POST /v1/reservations/quote", router.Policy{Roles: router.DriverOnly}, h.quote)
	r.Handle("POST /v1/reservations",
		router.Policy{Roles: router.DriverOnly, Limit: &ratelimit.ReservationWriteUser}, h.create)
	r.Handle("GET /v1/reservations", router.Policy{Roles: router.DriverOnly}, h.list)
	r.Handle("GET /v1/reservations/{id}", router.Policy{Roles: router.DriverOnly}, h.get)
	r.Handle("PATCH /v1/reservations/{id}",
		router.Policy{Roles: router.DriverOnly, Limit: &ratelimit.ReservationWriteUser}, h.changeVehicle)
	r.Handle("POST /v1/reservations/{id}/pay",
		router.Policy{Roles: router.DriverOnly, Limit: &ratelimit.ReservationPayUser}, h.pay)
	r.Handle("GET /v1/reservations/{id}/qr", router.Policy{Roles: router.DriverOnly}, h.qr)
}

// Ids arrive as strings: a malformed one is the same 404 as an unknown one
// (M3B-33), so it must not fail the decode.
type quoteRequest struct {
	LotID     string    `json:"lot_id" validate:"required"`
	ArrivalAt time.Time `json:"arrival_at" validate:"required"`
	VehicleID *string   `json:"vehicle_id"`
}

type createRequest struct {
	LotID     string    `json:"lot_id" validate:"required"`
	VehicleID string    `json:"vehicle_id" validate:"required"`
	ArrivalAt time.Time `json:"arrival_at" validate:"required"`
}

// idOrNil parses a public id; a malformed one becomes the nil uuid, which no
// row has.
func idOrNil(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

type windowDTO struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

func toWindowDTO(w Window) windowDTO { return windowDTO{StartsAt: w.Start.UTC(), EndsAt: w.End.UTC()} }

type quoteDTO struct {
	LotID             string    `json:"lot_id"`
	ArrivalAt         time.Time `json:"arrival_at"`
	HoldWindow        windowDTO `json:"hold_window"`
	FeeCents          int64     `json:"fee_cents"`
	PaymentTimeoutMin int       `json:"payment_timeout_min"`
}

type reservationVehicleDTO struct {
	ID           string `json:"id"`
	PlateDisplay string `json:"plate_display"`
	Make         string `json:"make"`
	Model        string `json:"model"`
	Color        string `json:"color"`
}

// reservationDTO is the design 7.1 representation. The nullable fields are
// always present; those a reservation cannot have yet are null.
type reservationDTO struct {
	ID            string                `json:"id"`
	LotID         string                `json:"lot_id"`
	Vehicle       reservationVehicleDTO `json:"vehicle"`
	ArrivalAt     time.Time             `json:"arrival_at"`
	HoldWindow    windowDTO             `json:"hold_window"`
	Status        string                `json:"status"`
	FeeCents      int64                 `json:"fee_cents"`
	PaymentDueAt  time.Time             `json:"payment_due_at"`
	CreatedAt     time.Time             `json:"created_at"`
	ConfirmedAt   *time.Time            `json:"confirmed_at"`
	CancelledAt   *time.Time            `json:"cancelled_at"`
	CancelledBy   *string               `json:"cancelled_by"`
	RefundReason  *string               `json:"refund_reason"`
	CheckedInAt   *time.Time            `json:"checked_in_at"`
	CompletedAt   *time.Time            `json:"completed_at"`
	ExpiredAt     *time.Time            `json:"expired_at"`
	LatestPayment any                   `json:"latest_payment"`
	Refund        any                   `json:"refund"`
	Stay          any                   `json:"stay"`
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func toReservationDTO(v View) reservationDTO {
	return reservationDTO{
		ID: v.ID.String(), LotID: v.LotID.String(),
		Vehicle: reservationVehicleDTO{
			ID: v.Vehicle.PublicID.String(), PlateDisplay: v.Vehicle.PlateDisplay,
			Make: v.Vehicle.Make, Model: v.Vehicle.Model, Color: v.Vehicle.Color,
		},
		ArrivalAt: v.ArrivalAt.UTC(), HoldWindow: toWindowDTO(v.Window), Status: string(v.Status),
		FeeCents: v.FeeCents, PaymentDueAt: v.PaymentDueAt.UTC(), CreatedAt: v.CreatedAt.UTC(),
		ConfirmedAt: utcPtr(v.ConfirmedAt), CancelledAt: utcPtr(v.CancelledAt), CancelledBy: v.CancelledBy,
		RefundReason: v.RefundReason, CheckedInAt: utcPtr(v.CheckedInAt), CompletedAt: utcPtr(v.CompletedAt),
		ExpiredAt: utcPtr(v.ExpiredAt),
	}
}

func (h *Handler) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	p, ok := authz.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, h.log, apperr.Unauthenticated(errcodes.AuthUnauthenticated))
		return 0, false
	}
	return p.UserID, true
}

func (h *Handler) quote(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(w, r)
	if !ok {
		return
	}
	req, err := httpx.DecodeJSON[quoteRequest](r, h.v)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	in := QuoteInput{LotID: idOrNil(req.LotID), ArrivalAt: req.ArrivalAt}
	if req.VehicleID != nil {
		id := idOrNil(*req.VehicleID)
		in.VehicleID = &id
	}
	q, err := h.svc.Quote(r.Context(), uid, in)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusOK, quoteDTO{
		LotID: q.LotID.String(), ArrivalAt: q.ArrivalAt.UTC(), HoldWindow: toWindowDTO(q.Window),
		FeeCents: q.FeeCents, PaymentTimeoutMin: q.PaymentTimeoutMin,
	})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(w, r)
	if !ok {
		return
	}
	req, err := httpx.DecodeJSON[createRequest](r, h.v)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.Create(r.Context(), uid, CreateInput{
		LotID: idOrNil(req.LotID), VehicleID: idOrNil(req.VehicleID), ArrivalAt: req.ArrivalAt,
	})
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusCreated, toReservationDTO(v))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(w, r)
	if !ok {
		return
	}
	v, err := h.svc.Get(r.Context(), uid, idOrNil(r.PathValue("id")))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusOK, toReservationDTO(v))
}

// list parses the query by hand: a bad limit is a 422 naming the field, like
// a bad scope or cursor. A missing scope is "active", as the contract says.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	in := ListQuery{Scope: ScopeActive, Limit: DefaultPageSize, Cursor: q.Get("cursor")}
	if s := q.Get("scope"); s != "" {
		in.Scope = s
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			httpx.WriteError(w, r, h.log, fieldError("limit", "numeric", ""))
			return
		}
		in.Limit = n
	}
	views, next, err := h.svc.List(r.Context(), uid, in)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	items := make([]reservationDTO, len(views))
	for i, v := range views {
		items[i] = toReservationDTO(v)
	}
	page := response.Page{Limit: in.Limit}
	if next != "" {
		page.NextCursor = &next
	}
	response.List(w, r, items, page)
}

type changeVehicleRequest struct {
	VehicleID string `json:"vehicle_id" validate:"required"`
}

func (h *Handler) changeVehicle(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(w, r)
	if !ok {
		return
	}
	req, err := httpx.DecodeJSON[changeVehicleRequest](r, h.v)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	v, err := h.svc.ChangeVehicle(r.Context(), uid, idOrNil(r.PathValue("id")), idOrNil(req.VehicleID))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusOK, toReservationDTO(v))
}
