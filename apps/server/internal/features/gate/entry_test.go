package gate_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
)

func TestNewTicketCode(t *testing.T) {
	code, err := gate.NewTicketCode(bytes.NewReader([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}))
	if err != nil || code != "000G40R40M30E209" {
		t.Fatalf("NewTicketCode(0x00..0x09) = %q, %v; want 000G40R40M30E209", code, err)
	}
	for range 200 {
		code, err := gate.NewTicketCode(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 16 || strings.Trim(code, "0123456789ABCDEFGHJKMNPQRSTVWXYZ") != "" {
			t.Fatalf("code %q is not 16 Crockford characters", code)
		}
		if n, ok := gate.NormalizeTicketCode(code); !ok || n != code {
			t.Fatalf("NormalizeTicketCode(%q) = %q, %v", code, n, ok)
		}
	}
}

func TestNewTicketCode_ShortReader(t *testing.T) {
	if _, err := gate.NewTicketCode(bytes.NewReader(make([]byte, 9))); err == nil {
		t.Fatal("9 random bytes gave a code, want an error")
	}
}

// The hash is over the normalized code, so a code typed in another form
// finds the same stay.
func TestHashTicketCode(t *testing.T) {
	want := sha256.Sum256([]byte("000G40R40M30E209"))
	for _, typed := range []string{"000G40R40M30E209", "000g-40r4-0m30-e209", "OOOG 4OR4 OM3O E2O9"} {
		if got := gate.HashTicketCode(typed); !bytes.Equal(got, want[:]) {
			t.Errorf("HashTicketCode(%q) differs from sha256 of the normalized code", typed)
		}
	}
}
