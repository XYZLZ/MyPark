package reservations

import (
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/keyset"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
)

// idempotencyKeyRe is the contract's Idempotency-Key pattern (design 7.6).
var idempotencyKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// Header names of the pay endpoint.
const (
	headerIdempotencyKey = "Idempotency-Key"
	headerReplayed       = "Idempotent-Replayed"
	// payRetryAfter is the Retry-After of a 202, in seconds.
	payRetryAfter = "2"
)

type payPendingDTO struct {
	Reservation reservationDTO `json:"reservation"`
	Payment     struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"payment"`
}

// pay is POST /v1/reservations/{id}/pay. Neither the card nor the key is ever
// logged or echoed: validation failures name the field and the rule only.
func (h *Handler) pay(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(w, r)
	if !ok {
		return
	}
	key := r.Header.Get(headerIdempotencyKey)
	switch {
	case key == "":
		httpx.WriteError(w, r, h.log, keyset.FieldError(headerIdempotencyKey, "required", ""))
		return
	case !idempotencyKeyRe.MatchString(key):
		httpx.WriteError(w, r, h.log, keyset.FieldError(headerIdempotencyKey, "idempotency_key", ""))
		return
	}
	req, err := httpx.DecodeJSON[payments.PayRequest](r, nil)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	req.CardNumber = payments.NormalizeCardNumber(req.CardNumber)
	// card_expiry fails closed without the business time.
	if err := httpx.ValidateCtx(validation.WithNow(r.Context(), h.svc.clock.Now()), h.v, req); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}

	res, err := h.svc.Pay(r.Context(), uid, idOrNil(r.PathValue("id")), key, req)
	if err != nil {
		if _, ok := errors.AsType[*ReplayedError](err); ok {
			w.Header().Set(headerReplayed, "true")
		}
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if res.Replayed {
		w.Header().Set(headerReplayed, "true")
	}
	switch res.Code {
	case http.StatusOK:
		response.OK(w, r, http.StatusOK, toReservationDTO(res.Reservation))
	case http.StatusAccepted:
		w.Header().Set("Retry-After", payRetryAfter)
		body := payPendingDTO{Reservation: toReservationDTO(res.Reservation)}
		body.Payment.ID, body.Payment.Status = res.PaymentID.String(), payments.StatusPending
		response.OK(w, r, http.StatusAccepted, body)
	default:
		httpx.WriteError(w, r, h.log, apperr.PaymentRequired(errcodes.PaymentDeclined).
			WithDetails(map[string]any{"payment_id": res.PaymentID.String(), "failure_code": res.FailureCode}))
	}
}

type qrDTO struct {
	Token           string    `json:"token"`
	EntryValidFrom  time.Time `json:"entry_valid_from"`
	EntryValidUntil time.Time `json:"entry_valid_until"`
}

// qr is GET /v1/reservations/{id}/qr. The token is a secret: it is sent with
// no-store and never logged.
func (h *Handler) qr(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userID(w, r)
	if !ok {
		return
	}
	v, err := h.svc.QR(r.Context(), uid, idOrNil(r.PathValue("id")))
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.OK(w, r, http.StatusOK, qrDTO{
		Token: v.Token, EntryValidFrom: v.EntryValidFrom.UTC(), EntryValidUntil: v.EntryValidUntil.UTC(),
	})
}
