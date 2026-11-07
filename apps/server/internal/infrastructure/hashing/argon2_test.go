package hashing

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

var fastParams = Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func TestHash_PHCFormatAndRoundTrip(t *testing.T) {
	h := NewArgon2id(DefaultParams, 4)
	phc, err := h.Hash(t.Context(), "Zx9!kelp-orbit")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(phc, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected PHC prefix: %q", phc)
	}
	if strings.Contains(phc, "=") && strings.HasSuffix(phc, "=") {
		t.Fatalf("PHC must use unpadded base64: %q", phc)
	}
	ok, needsRehash, err := h.Verify(t.Context(), phc, "Zx9!kelp-orbit")
	if err != nil || !ok || needsRehash {
		t.Fatalf("Verify = %v, %v, %v; want true, false, nil", ok, needsRehash, err)
	}
}

func TestHash_UsesFreshSalt(t *testing.T) {
	h := NewArgon2id(fastParams, 2)
	a, _ := h.Hash(t.Context(), "same-password")
	b, _ := h.Hash(t.Context(), "same-password")
	if a == b {
		t.Fatal("two hashes of the same password must differ")
	}
}

func TestVerify_WrongPassword(t *testing.T) {
	h := NewArgon2id(fastParams, 2)
	phc, _ := h.Hash(t.Context(), "right-password")
	ok, _, err := h.Verify(t.Context(), phc, "wrong-password")
	if err != nil || ok {
		t.Fatalf("Verify = %v, %v; want false, nil", ok, err)
	}
}

func flipChar(s string) string {
	b := []byte(s)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	return string(b)
}

func TestVerify_TamperedSaltOrHash(t *testing.T) {
	h := NewArgon2id(fastParams, 2)
	phc, _ := h.Hash(t.Context(), "right-password")
	parts := strings.Split(phc, "$") // "", argon2id, v, params, salt, hash
	for _, idx := range []int{4, 5} {
		p := slicesClone(parts)
		p[idx] = flipChar(p[idx])
		ok, _, err := h.Verify(t.Context(), strings.Join(p, "$"), "right-password")
		if err != nil || ok {
			t.Fatalf("tampered part %d: Verify = %v, %v; want false, nil", idx, ok, err)
		}
	}
}

func slicesClone(s []string) []string { return append([]string(nil), s...) }

func TestVerify_MalformedPHC(t *testing.T) {
	h := NewArgon2id(fastParams, 2)
	cases := map[string]string{
		"wrong algorithm": "$argon2i$v=19$m=1024,t=1,p=1$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNo",
		"empty":           "",
		"missing fields":  "$argon2id$v=19$m=1024,t=1,p=1$c2FsdA",
		"bad version":     "$argon2id$v=18$m=1024,t=1,p=1$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNo",
		"bad params":      "$argon2id$v=19$m=x,t=1,p=1$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNo",
		"bad base64":      "$argon2id$v=19$m=1024,t=1,p=1$!!!$aGFzaGhhc2hoYXNo",
		"zero threads":    "$argon2id$v=19$m=1024,t=1,p=0$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNo",
		"huge memory":     "$argon2id$v=19$m=4294967295,t=1,p=1$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNo",
	}
	for name, phc := range cases {
		t.Run(name, func(t *testing.T) {
			ok, _, err := h.Verify(t.Context(), phc, "pw")
			if err == nil || ok {
				t.Fatalf("Verify = %v, %v; want error", ok, err)
			}
		})
	}
}

func TestVerify_NeedsRehashOnOldParams(t *testing.T) {
	old := NewArgon2id(Params{Memory: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}, 2)
	phc, _ := old.Hash(t.Context(), "right-password")
	cur := NewArgon2id(Params{Memory: 2048, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}, 2)
	ok, needsRehash, err := cur.Verify(t.Context(), phc, "right-password")
	if err != nil || !ok || !needsRehash {
		t.Fatalf("Verify = %v, %v, %v; want true, true, nil", ok, needsRehash, err)
	}
}

func TestArgon2id_SemaphoreBoundsConcurrency(t *testing.T) {
	h := NewArgon2id(fastParams, 4)
	var inflight, peak atomic.Int32
	h.trace = func(delta int32) {
		n := inflight.Add(delta)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				return
			}
		}
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if _, err := h.Hash(context.Background(), "pw-concurrency"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if got := peak.Load(); got > 4 || got < 1 {
		t.Fatalf("peak in-flight = %d; want 1..4", got)
	}
}

func TestArgon2id_WaitRespectsContext(t *testing.T) {
	h := NewArgon2id(fastParams, 1)
	h.sem <- struct{}{} // hold the only slot
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := h.Hash(ctx, "pw"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Hash err = %v; want context.Canceled", err)
	}
	phc := "$argon2id$v=19$m=1024,t=1,p=1$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g"
	if _, _, err := h.Verify(ctx, phc, "pw"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify err = %v; want context.Canceled", err)
	}
}

func TestDummyHash_VerifiesNothingRealButParses(t *testing.T) {
	h := NewArgon2id(fastParams, 1)
	ok, _, err := h.Verify(t.Context(), h.DummyHash(), "anything")
	if err != nil || ok {
		t.Fatalf("Verify(dummy) = %v, %v; want false, nil", ok, err)
	}
}
