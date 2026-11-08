package hashing

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCodeHash_BindsPurposeAndUser(t *testing.T) {
	ch := mustHasher(t, "pepper-pepper-pepper-pepper-pepper")
	u1, u2 := uuid.New(), uuid.New()
	base := ch.Hash("verify", u1, "123456")
	if !Equal(base, ch.Hash("verify", u1, "123456")) {
		t.Fatal("same inputs must hash equal")
	}
	if Equal(base, ch.Hash("reset", u1, "123456")) {
		t.Fatal("different purpose must differ")
	}
	if Equal(base, ch.Hash("verify", u2, "123456")) {
		t.Fatal("different user must differ")
	}
	if Equal(base, ch.Hash("verify", u1, "654321")) {
		t.Fatal("different code must differ")
	}
	other := mustHasher(t, "another-pepper-value-another-pepper")
	if Equal(base, other.Hash("verify", u1, "123456")) {
		t.Fatal("different pepper must differ")
	}
	if len(base) != 32 {
		t.Fatalf("len = %d; want 32", len(base))
	}
}

func TestEqual_LengthMismatch(t *testing.T) {
	if Equal([]byte("a"), []byte("ab")) {
		t.Fatal("different lengths must not be equal")
	}
}

func mustHasher(t *testing.T, pepper string) CodeHasher {
	t.Helper()
	ch, err := NewCodeHasher([]byte(pepper))
	if err != nil {
		t.Fatalf("NewCodeHasher: %v", err)
	}
	return ch
}

func TestNewCodeHasher_RejectsShortPepper(t *testing.T) {
	for _, p := range [][]byte{nil, {}, []byte("short"), make([]byte, MinPepperLen-1)} {
		if _, err := NewCodeHasher(p); err == nil {
			t.Errorf("pepper of %d bytes accepted", len(p))
		}
	}
	if _, err := NewCodeHasher(make([]byte, MinPepperLen)); err != nil {
		t.Errorf("pepper of %d bytes rejected: %v", MinPepperLen, err)
	}
}

func TestCodeHasher_FormattingIsRedacted(t *testing.T) {
	const pepper = "super-secret-pepper-value-1234567890"
	ch := mustHasher(t, pepper)
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		for _, val := range []any{ch, &ch} {
			out := fmt.Sprintf(verb, val)
			if strings.Contains(out, pepper) || strings.Contains(out, fmt.Sprintf("%v", []byte(pepper))) {
				t.Errorf("%s leaks pepper: %s", verb, out)
			}
			if !strings.Contains(out, "[redacted]") {
				t.Errorf("%s = %q; want redacted", verb, out)
			}
		}
	}
}
