package hashing

import (
	"testing"

	"github.com/google/uuid"
)

func TestCodeHash_BindsPurposeAndUser(t *testing.T) {
	ch := NewCodeHasher([]byte("pepper-pepper-pepper"))
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
	other := NewCodeHasher([]byte("another-pepper-value"))
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
