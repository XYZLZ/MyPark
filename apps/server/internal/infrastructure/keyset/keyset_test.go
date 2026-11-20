package keyset_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/keyset"
)

func TestCursor_RoundTrip(t *testing.T) {
	at := time.Date(2026, 11, 16, 15, 4, 5, 123456789, time.FixedZone("x", -4*3600))
	id := uuid.New()
	gotAt, gotID, err := keyset.Decode(keyset.Encode(at, id))
	if err != nil || !gotAt.Equal(at) || gotID != id {
		t.Fatalf("round trip = %v %v %v, want %v %v", gotAt, gotID, err, at, id)
	}
}

func TestCursor_Tampered(t *testing.T) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	good := keyset.Encode(time.Now(), uuid.New())
	for name, in := range map[string]string{
		"not base64":   "!!!",
		"no separator": enc("nope"),
		"bad time":     enc("yesterday|" + uuid.NewString()),
		"bad id":       enc(time.Now().Format(time.RFC3339Nano) + "|abc"),
		"truncated":    good[:len(good)-5],
		"oversized":    strings.Repeat("A", 4096),
	} {
		if _, _, err := keyset.Decode(in); !errors.Is(err, keyset.ErrBadCursor) {
			t.Errorf("%s: err = %v, want ErrBadCursor", name, err)
		}
		if _, _, err := keyset.DecodeField(in); err == nil || !isInvalid(err) {
			t.Errorf("%s: DecodeField = %v, want a 422", name, err)
		}
	}
}

func TestLimit(t *testing.T) {
	if n, err := keyset.ParseLimit(""); n != keyset.DefaultLimit || err != nil {
		t.Errorf("empty = %d %v", n, err)
	}
	if _, err := keyset.ParseLimit("x"); err == nil {
		t.Error("non-number accepted")
	}
	for n, ok := range map[int]bool{0: false, 1: true, 50: true, 51: false} {
		if err := keyset.CheckLimit(n); (err == nil) != ok {
			t.Errorf("CheckLimit(%d) = %v", n, err)
		}
	}
}

func isInvalid(err error) bool {
	ae, ok := errors.AsType[*apperr.Error](err)
	return ok && ae.Kind == apperr.KindInvalid
}
