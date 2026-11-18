package reservations_test

import (
	"context"
	"errors"
	"time"

	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/reservations"
)

type qrGolden struct {
	KeyHex   string `json:"key_hex"`
	PublicID string `json:"public_id"`
	NonceHex string `json:"nonce_hex"`
	Token    string `json:"token"`
}

func loadGolden(t *testing.T) (key []byte, id uuid.UUID, nonce [16]byte, token string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/qr_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g qrGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	key, _ = hex.DecodeString(g.KeyHex)
	id = uuid.MustParse(g.PublicID)
	n, _ := hex.DecodeString(g.NonceHex)
	copy(nonce[:], n)
	return key, id, nonce, g.Token
}

// The token was computed outside Go (node crypto, HMAC-SHA256 over
// public_id || nonce, base64url without padding) and frozen.
func TestQR_GoldenVector(t *testing.T) {
	key, id, nonce, want := loadGolden(t)
	got := reservations.EncodeQR(key, id, nonce)
	if got != want {
		t.Errorf("token = %s, want %s", got, want)
	}
	if len(got) != 86 {
		t.Errorf("len = %d, want 86", len(got))
	}
	gotID, gotNonce, ok := reservations.DecodeQR(key, want)
	if !ok || gotID != id || gotNonce != nonce {
		t.Errorf("decode golden = %v %x %v", gotID, gotNonce, ok)
	}
}

func TestQR_RoundTripAndTamper(t *testing.T) {
	key, id, nonce, token := loadGolden(t)
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 64 {
		t.Fatalf("golden decodes to %d bytes: %v", len(raw), err)
	}

	for i := range raw {
		b := append([]byte(nil), raw...)
		b[i] ^= 0x01
		if _, _, ok := reservations.DecodeQR(key, base64.RawURLEncoding.EncodeToString(b)); ok {
			t.Errorf("flipping byte %d still decodes", i)
		}
	}

	otherKey := append([]byte(nil), key...)
	otherKey[0] ^= 0xff
	cases := map[string]struct {
		key   []byte
		token string
	}{
		"wrong key":      {otherKey, token},
		"85 chars":       {key, token[:85]},
		"87 chars":       {key, token + "A"},
		"padding":        {key, token[:84] + "=="},
		"plus":           {key, "+" + token[1:]},
		"slash":          {key, "/" + token[1:]},
		"trailing LF":    {key, token[:85] + "\n"},
		"trailing LF 87": {key, token + "\n"},
		"embedded CR":    {key, token[:40] + "\r" + token[41:]},
		"leading space":  {key, " " + token[1:]},
		"empty":          {key, ""},
		"non canonical":  {key, token[:85] + "B"},
	}
	for name, c := range cases {
		if _, _, ok := reservations.DecodeQR(c.key, c.token); ok {
			t.Errorf("%s: decoded, want rejected", name)
		}
	}

	if gotID, gotNonce, ok := reservations.DecodeQR(key, reservations.EncodeQR(key, id, nonce)); !ok || gotID != id || gotNonce != nonce {
		t.Errorf("round trip = %v %x %v", gotID, gotNonce, ok)
	}
	if strings.ContainsAny(token, "=+/\n") {
		t.Errorf("token has characters outside base64url: %q", token)
	}
}

// qrStubRepo answers the three calls Service.QR makes.
type qrStubRepo struct {
	reservations.Repo
	data   reservations.QRData
	err    error
	limits error
}

func (r qrStubRepo) SetTxLimits(context.Context) error { return r.limits }
func (r qrStubRepo) QRData(context.Context, int64, uuid.UUID) (reservations.QRData, error) {
	return r.data, r.err
}

type passTx struct{}

func (passTx) InTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

func TestService_QR(t *testing.T) {
	key, id, nonce, token := loadGolden(t)
	win := reservations.Window{
		Start: time.Date(2026, 12, 1, 14, 50, 0, 0, time.UTC), End: time.Date(2026, 12, 1, 15, 25, 0, 0, time.UTC),
	}
	svc := func(r qrStubRepo) *reservations.Service {
		return reservations.NewService(reservations.Deps{Tx: passTx{}, Repo: r, QRKey: key})
	}
	data := func(status reservations.Status, n []byte) reservations.QRData {
		return reservations.QRData{PublicID: id, Status: status, Nonce: n, Window: win}
	}
	kind := func(t *testing.T, err error) (apperr.Kind, errcodes.Code) {
		t.Helper()
		ae, ok := errors.AsType[*apperr.Error](err)
		if !ok {
			t.Fatalf("error = %v, want an *apperr.Error", err)
		}
		return ae.Kind, ae.Code
	}

	for _, st := range []reservations.Status{reservations.StatusConfirmed, reservations.StatusCheckedIn} {
		got, err := svc(qrStubRepo{data: data(st, nonce[:])}).QR(t.Context(), 1, id)
		if err != nil || got.Token != token || !got.EntryValidFrom.Equal(win.Start) || !got.EntryValidUntil.Equal(win.End) {
			t.Errorf("%s: QR = %+v, %v; want the golden token and the window", st, got, err)
		}
	}
	if _, err := svc(qrStubRepo{err: reservations.ErrNotFound}).QR(t.Context(), 1, id); err == nil {
		t.Error("an unknown reservation gave no error")
	} else if k, c := kind(t, err); k != apperr.KindNotFound || c != errcodes.ReservationNotFound {
		t.Errorf("unknown = %v %v, want not found reservation.not_found", k, c)
	}
	for _, st := range []reservations.Status{reservations.StatusPendingPayment, reservations.StatusCancelled} {
		_, err := svc(qrStubRepo{data: data(st, nonce[:])}).QR(t.Context(), 1, id)
		if k, c := kind(t, err); k != apperr.KindConflict || c != errcodes.ReservationInvalidState {
			t.Errorf("%s = %v %v, want conflict reservation.invalid_state", st, k, c)
		}
	}
	boom := errors.New("db down")
	if _, err := svc(qrStubRepo{err: boom}).QR(t.Context(), 1, id); !errors.Is(err, boom) {
		t.Errorf("a repo error = %v, want it passed through", err)
	}
	if _, err := svc(qrStubRepo{limits: boom}).QR(t.Context(), 1, id); !errors.Is(err, boom) {
		t.Errorf("a SetTxLimits error = %v, want it passed through", err)
	}
	if _, err := svc(qrStubRepo{data: data(reservations.StatusConfirmed, nonce[:3])}).QR(t.Context(), 1, id); err == nil {
		t.Error("a 3-byte nonce produced a token")
	} else if _, isApp := errors.AsType[*apperr.Error](err); isApp {
		t.Errorf("a corrupt nonce = %v, want a plain internal error", err)
	}
}
