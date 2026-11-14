package devices

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/secretbox"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/response"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// HeaderReplayed marks the stored answer of an earlier call.
const HeaderReplayed = "Idempotent-Replayed"

// txLimits bounds one event transaction (design §8.1): a timeout rolls back,
// answers 500 and stores nothing, so the device retries the same event id.
const txLimits = `select set_config('statement_timeout', '2s', true), set_config('lock_timeout', '1s', true)`

// HandlerDeps are the Handler's dependencies.
type HandlerDeps struct {
	Tx     database.TxRunnerPort
	Store  Store
	Gate   gate.Handler
	Sealer *secretbox.Sealer
	Clock  clock.Clock
	Log    *slog.Logger
}

// Handler serves POST /v1/devices/events.
type Handler struct {
	tx     database.TxRunnerPort
	store  Store
	gate   gate.Handler
	sealer *secretbox.Sealer
	clock  clock.Clock
	log    *slog.Logger
}

// NewHandler returns a Handler over d.
func NewHandler(d HandlerDeps) *Handler {
	return &Handler{tx: d.Tx, store: d.Store, gate: d.Gate, sealer: d.Sealer, clock: d.Clock, log: d.Log}
}

// Register mounts the device event route behind the device HMAC policy.
func (h *Handler) Register(r *router.Router) {
	r.Handle("POST /v1/devices/events", router.Policy{Device: true}, h.postEvent)
}

// postEvent runs one device event (design §4.3, §7.4): decode, kind matrix,
// replay check, then one transaction that handles the event and stores it
// last. Only a 200 answer is stored; any error stores nothing.
func (h *Handler) postEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dev, ok := authz.DeviceFrom(ctx)
	if !ok {
		httpx.WriteError(w, r, h.log, errors.New("devices: event route reached without a device"))
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		httpx.WriteError(w, r, h.log, apperr.New(apperr.KindUnsupportedMedia, errcodes.HTTPUnsupportedMediaType))
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxEventBytes))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			httpx.WriteError(w, r, h.log, apperr.New(apperr.KindTooLarge, errcodes.HTTPPayloadTooLarge))
			return
		}
		httpx.WriteError(w, r, h.log, apperr.New(apperr.KindBadRequest, errcodes.HTTPInvalidJSON).Wrap(err))
		return
	}
	ev, err := DecodeEvent(raw, h.clock.Now())
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	if !Allowed(dev.Kind, ev) {
		httpx.WriteError(w, r, h.log, apperr.Forbidden(errcodes.DeviceEventNotAllowed))
		return
	}
	sum := sha256.Sum256(raw)

	if h.replay(w, r, dev.ID, ev.ID, sum[:]) {
		return
	}
	res, err := h.process(ctx, dev, ev, sum[:])
	if errors.Is(err, ErrDuplicateEvent) {
		// A concurrent duplicate committed first. Its transaction is over,
		// so the stored answer is now visible.
		if h.replay(w, r, dev.ID, ev.ID, sum[:]) {
			return
		}
		err = fmt.Errorf("devices: event %s vanished after a duplicate insert", ev.ID)
	}
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	response.OK(w, r, http.StatusOK, res)
}

// replay answers from the stored event when there is one and reports whether
// it did: the opened stored answer for the same body, 409 for another body.
func (h *Handler) replay(w http.ResponseWriter, r *http.Request, deviceID int64, eventID uuid.UUID, sum []byte) bool {
	stored, found, err := h.store.FindEvent(r.Context(), deviceID, eventID)
	switch {
	case err != nil:
		httpx.WriteError(w, r, h.log, err)
		return true
	case !found:
		return false
	case !bytes.Equal(stored.BodySHA256, sum):
		httpx.WriteError(w, r, h.log, apperr.Conflict(errcodes.DeviceEventConflict))
		return true
	}
	data, err := OpenResult(h.sealer, stored.Response)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return true
	}
	w.Header().Set(HeaderReplayed, "true")
	response.OK(w, r, http.StatusOK, json.RawMessage(data))
	return true
}

// process handles ev and stores it in one short transaction. The insert is
// the last statement (M2C-29), so a duplicate is only detected after the
// gate's locks, and its rollback undoes the gate's writes.
func (h *Handler) process(ctx context.Context, dev authz.Device, ev gate.Event, sum []byte) (gate.Result, error) {
	payload, err := RedactPayload(ev)
	if err != nil {
		return gate.Result{}, err
	}
	var res gate.Result
	err = h.tx.InTx(ctx, func(ctx context.Context) error {
		tx, err := database.MustTx(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, txLimits); err != nil {
			return fmt.Errorf("devices: set transaction limits: %w", err)
		}
		if res, err = h.gate.Handle(ctx, dev, ev); err != nil {
			return err
		}
		stored, err := SealResult(h.sealer, res)
		if err != nil {
			return err
		}
		return h.store.InsertEvent(ctx, NewEvent{
			LotID: dev.LotID, DeviceID: dev.ID, EventID: ev.ID, Type: ev.Type, OccurredAt: ev.OccurredAt,
			BodySHA256: sum, Payload: payload, Response: stored,
		})
	})
	return res, err
}
