package payments

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// ReceiptHandler serves the receipt endpoints.
type ReceiptHandler struct {
	svc *ReceiptService
	log *slog.Logger
}

// NewReceiptHandler returns a ReceiptHandler.
func NewReceiptHandler(svc *ReceiptService, log *slog.Logger) *ReceiptHandler {
	return &ReceiptHandler{svc: svc, log: log}
}

// Register adds the receipt routes to r: driver-only, behind the legal gate.
func (h *ReceiptHandler) Register(r *router.Router) {
	r.Handle("GET /v1/payments", router.Policy{Roles: router.DriverOnly}, h.list)
	r.Handle("GET /v1/payments/{id}", router.Policy{Roles: router.DriverOnly}, h.get)
}

type receiptRefundDTO struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	AmountCents int64      `json:"amount_cents"`
	Reason      string     `json:"reason"`
	SucceededAt *time.Time `json:"succeeded_at"`
}

type receiptDTO struct {
	ID            string            `json:"id"`
	ReceiptNo     *string           `json:"receipt_no"`
	Kind          string            `json:"kind"`
	Method        string            `json:"method"`
	AmountCents   int64             `json:"amount_cents"`
	Status        string            `json:"status"`
	CardBrand     *string           `json:"card_brand"`
	CardLast4     *string           `json:"card_last4"`
	FailureCode   *string           `json:"failure_code"`
	ReservationID *string           `json:"reservation_id"`
	CreatedAt     time.Time         `json:"created_at"`
	SucceededAt   *time.Time        `json:"succeeded_at"`
	RefundedAt    *time.Time        `json:"refunded_at"`
	Refund        *receiptRefundDTO `json:"refund"`
}

// strPtr is nil for "", the JSON null of an unset text.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func toReceiptDTO(r Receipt) receiptDTO {
	out := receiptDTO{
		ID: r.ID.String(), ReceiptNo: strPtr(r.ReceiptNo), Kind: r.Kind, Method: r.Method,
		AmountCents: r.AmountCents, Status: r.Status, CardBrand: strPtr(r.CardBrand), CardLast4: strPtr(r.CardLast4),
		FailureCode: strPtr(r.FailureCode), CreatedAt: r.CreatedAt.UTC(),
		SucceededAt: utcPtr(r.SucceededAt), RefundedAt: utcPtr(r.RefundedAt),
	}
	if r.ReservationID != nil {
		id := r.ReservationID.String()
		out.ReservationID = &id
	}
	if rf := r.Refund; rf != nil {
		out.Refund = &receiptRefundDTO{
			ID: rf.PublicID.String(), Status: rf.Status, AmountCents: rf.AmountCents, Reason: rf.Reason,
			SucceededAt: utcPtr(rf.SucceededAt),
		}
	}
	return out
}

func (h *ReceiptHandler) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	p, ok := authz.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, h.log, apperr.Unauthenticated(errcodes.AuthUnauthenticated))
		return 0, false
	}
	return p.UserID, true
}

// list parses the query by hand: a bad limit is a 422 naming the field, like a
// bad cursor.
func (h *ReceiptHandler) list(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit := DefaultReceiptPageSize
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			httpx.WriteError(w, r, h.log, fieldError("limit", "numeric", ""))
			return
		}
		limit = n
	}
	recs, next, err := h.svc.List(r.Context(), uid, limit, q.Get("cursor"))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	items := make([]receiptDTO, len(recs))
	for i, rec := range recs {
		items[i] = toReceiptDTO(rec)
	}
	page := response.Page{Limit: limit}
	if next != "" {
		page.NextCursor = &next
	}
	response.List(w, r, items, page)
}

func (h *ReceiptHandler) get(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(w, r)
	if !ok {
		return
	}
	// A malformed id is the same 404 as an unknown one (M3B-33).
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		id = uuid.Nil
	}
	rec, err := h.svc.Get(r.Context(), uid, id)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusOK, toReceiptDTO(rec))
}
