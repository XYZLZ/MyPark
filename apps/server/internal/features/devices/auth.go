package devices

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicesig"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/clock"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/secretbox"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/httpx"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
)

// Device request headers (design §7.1).
const (
	HeaderDeviceID  = "X-Device-Id"
	HeaderTimestamp = "X-Timestamp"
	HeaderSignature = "X-Signature"
)

const (
	// maxSkew is the accepted distance between X-Timestamp and the server clock.
	maxSkew = 60 * time.Second
	// touchEvery throttles last_seen_at writes (the query throttles too).
	touchEvery = 30 * time.Second
)

// dummyKey is the fixed key unknown and revoked devices are verified against,
// so they cost the same HMAC as a known device (M2C-17). Its result is always
// discarded: a request signed with it never authenticates.
var dummyKey = sha256.Sum256([]byte("mypark/devices/dummy-hmac-key"))

// Authenticator is the device HMAC middleware (design §7.3).
type Authenticator struct {
	reg    Registry
	sealer *secretbox.Sealer
	clock  clock.Clock
	log    *slog.Logger
	verify func(secret, canonical []byte, sig string) bool
}

// NewAuthenticator returns an Authenticator that looks devices up in reg and
// opens their secrets with s.
func NewAuthenticator(reg Registry, s *secretbox.Sealer, clk clock.Clock, log *slog.Logger) *Authenticator {
	return &Authenticator{reg: reg, sealer: s, clock: clk, log: log, verify: devicesig.Verify}
}

// Middleware authenticates the request, in order: headers, clock window, body
// read (capped), registry lookup and constant-time MAC check. Every identity
// failure is the same 401 device.unauthenticated; only the clock window has
// its own code. A user bearer token is never consulted. On success the device
// is in the context, LogFields.DeviceID is set and r.Body holds the read bytes.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, idOK := parseDeviceID(r.Header.Get(HeaderDeviceID))
		ts := r.Header.Get(HeaderTimestamp)
		at, tsOK := devicesig.ParseTimestamp(ts)
		sig := r.Header.Get(HeaderSignature)
		if !idOK || !tsOK || !devicesig.WellFormedSignature(sig) {
			a.refuse(w, r, id, "headers", errcodes.DeviceUnauthenticated)
			return
		}
		if skew := a.clock.Now().Sub(at); skew > maxSkew || skew < -maxSkew {
			a.refuse(w, r, id, "skew", errcodes.DeviceTimestampOutOfWindow)
			return
		}

		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxEventBytes))
		if err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				httpx.WriteError(w, r, a.log, apperr.New(apperr.KindTooLarge, errcodes.HTTPPayloadTooLarge))
				return
			}
			httpx.WriteError(w, r, a.log, apperr.New(apperr.KindBadRequest, errcodes.HTTPInvalidJSON).Wrap(err))
			return
		}
		// r.RequestURI is the request-target exactly as sent; r.URL.RequestURI() re-encodes it, so a valid signature over the raw query could fail.
		canonical := devicesig.Canonical(r.Method, r.RequestURI, ts, raw)

		rec, err := a.reg.DeviceByPublicID(r.Context(), id)
		if errors.Is(err, ErrDeviceNotFound) || (err == nil && rec.Revoked) {
			a.verify(dummyKey[:], canonical, sig) // equal work; the result is discarded
			reason := "unknown"
			if err == nil {
				reason = "revoked"
			}
			a.refuse(w, r, id, reason, errcodes.DeviceUnauthenticated)
			return
		}
		if err != nil {
			httpx.WriteError(w, r, a.log, err)
			return
		}
		secret, err := a.sealer.Open(rec.SecretCiphertext, rec.SecretKeyID)
		if err != nil {
			httpx.WriteError(w, r, a.log, fmt.Errorf("devices: open secret of %s: %w", id, err))
			return
		}
		ok := a.verify(secret, canonical, sig)
		clear(secret)
		if !ok {
			a.refuse(w, r, id, "signature", errcodes.DeviceUnauthenticated)
			return
		}

		a.touch(r, rec)
		ctx := authz.WithDevice(r.Context(), authz.Device{ID: rec.ID, PublicID: rec.PublicID, LotID: rec.LotID, Kind: rec.Kind})
		if f := reqctx.Fields(ctx); f != nil {
			f.DeviceID = rec.PublicID.String()
		}
		r = r.WithContext(ctx)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		r.ContentLength = int64(len(raw))
		next.ServeHTTP(w, r)
	})
}

// touch records activity when the stored last_seen_at is older than
// touchEvery. It is best effort: a failure is logged and the request goes on.
func (a *Authenticator) touch(r *http.Request, rec Record) {
	now := a.clock.Now()
	if !rec.LastSeenAt.IsZero() && !rec.LastSeenAt.Before(now.Add(-touchEvery)) {
		return
	}
	if err := a.reg.TouchLastSeen(r.Context(), rec.ID, now); err != nil {
		a.log.WarnContext(r.Context(), "devices: touch last seen failed", "device_id", rec.PublicID.String(), "err", err)
	}
}

// refuse logs the failure reason and answers 401 without saying which check failed.
func (a *Authenticator) refuse(w http.ResponseWriter, r *http.Request, claimed uuid.UUID, reason string, code errcodes.Code) {
	claimedID := ""
	if claimed != uuid.Nil {
		claimedID = claimed.String()
	}
	a.log.WarnContext(r.Context(), "device auth failed",
		"claimed_device_id", claimedID,
		"reason", reason,
		// Never the raw IP: the rate limiter's hash of the device_ip bucket key,
		// so it matches that rule's throttle logs (key_hash).
		"client_ip_hash", hex.EncodeToString(ratelimit.HashKey(ratelimit.DeviceIP, ratelimit.RuleIPKey(ratelimit.DeviceIP, reqctx.ClientIP(r.Context())))))
	httpx.WriteError(w, r, a.log, apperr.Unauthenticated(code))
}

// parseDeviceID accepts only the canonical 36-character uuid form, so the
// logged claimed id is always clean. uuid.Parse alone also takes urn, brace
// and 32-hex forms.
func parseDeviceID(s string) (uuid.UUID, bool) {
	if len(s) != 36 {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(s)
	return id, err == nil
}
