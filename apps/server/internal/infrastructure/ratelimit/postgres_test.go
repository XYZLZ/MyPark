package ratelimit_test

import (
	"bytes"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// start is 10 s into a one-minute window, so a fresh bucket expires in 50 s.
var start = time.Date(2026, 11, 20, 12, 0, 10, 0, time.UTC)

func newPostgres(t *testing.T) (*ratelimit.Postgres, *testsupport.FakeClock, *testsupport.TestDB) {
	t.Helper()
	db := testsupport.NewTestDB(t)
	clk := testsupport.NewFakeClock(start)
	return ratelimit.NewPostgres(db.App, clk), clk, db
}

func TestPostgres_SixthLoginIn429(t *testing.T) {
	l, _, _ := newPostgres(t)
	for i := range 5 {
		d, err := l.Allow(t.Context(), ratelimit.LoginIPEmail, "203.0.113.9|ana@example.com")
		if err != nil || !d.Allowed {
			t.Fatalf("attempt %d = %+v, %v; want allowed", i+1, d, err)
		}
	}
	d, err := l.Allow(t.Context(), ratelimit.LoginIPEmail, "203.0.113.9|ana@example.com")
	if err != nil {
		t.Fatalf("sixth attempt: %v", err)
	}
	if d.Allowed {
		t.Fatal("sixth attempt allowed, want denied")
	}
	if d.RetryAfter != 50*time.Second {
		t.Errorf("RetryAfter = %v, want 50s (window ends at 12:01:00)", d.RetryAfter)
	}
	// Another key has its own bucket.
	if d, err := l.Allow(t.Context(), ratelimit.LoginIPEmail, "203.0.113.9|bob@example.com"); err != nil || !d.Allowed {
		t.Errorf("other key = %+v, %v; want allowed", d, err)
	}
}

func TestPostgres_WindowRollsOverWithFakeClock(t *testing.T) {
	l, clk, _ := newPostgres(t)
	for range 6 {
		if _, err := l.Allow(t.Context(), ratelimit.LoginIPEmail, "k"); err != nil {
			t.Fatal(err)
		}
	}
	if d, _ := l.Allow(t.Context(), ratelimit.LoginIPEmail, "k"); d.Allowed {
		t.Fatal("seventh attempt allowed inside the window")
	}
	clk.Advance(50 * time.Second) // exactly the next window
	for i := range 5 {
		d, err := l.Allow(t.Context(), ratelimit.LoginIPEmail, "k")
		if err != nil || !d.Allowed {
			t.Fatalf("new-window attempt %d = %+v, %v; want allowed", i+1, d, err)
		}
	}
	if d, _ := l.Allow(t.Context(), ratelimit.LoginIPEmail, "k"); d.Allowed {
		t.Error("sixth attempt in the new window allowed, want denied")
	}
}

func TestPostgres_TwoInstancesShareCount(t *testing.T) {
	_, clk, db := newPostgres(t)
	second, err := pgxpool.New(t.Context(), db.AppURL)
	if err != nil {
		t.Fatalf("second pool: %v", err)
	}
	t.Cleanup(second.Close)
	a, b := ratelimit.NewPostgres(db.App, clk), ratelimit.NewPostgres(second, clk)
	for i := range 5 {
		l := a
		if i%2 == 1 {
			l = b
		}
		if d, err := l.Allow(t.Context(), ratelimit.LoginIPEmail, "k"); err != nil || !d.Allowed {
			t.Fatalf("attempt %d = %+v, %v; want allowed", i+1, d, err)
		}
	}
	if d, err := b.Allow(t.Context(), ratelimit.LoginIPEmail, "k"); err != nil || d.Allowed {
		t.Errorf("sixth attempt (instance b) = %+v, %v; want denied", d, err)
	}
}

func TestPostgres_KeysAreHashed(t *testing.T) {
	l, _, db := newPostgres(t)
	const key = "203.0.113.9|ana@example.com"
	if _, err := l.Allow(t.Context(), ratelimit.LoginIPEmail, key); err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err := db.Super.QueryRow(t.Context(), `select key from rate_limit_counters`).Scan(&stored); err != nil {
		t.Fatalf("select key: %v", err)
	}
	want := sha256.Sum256([]byte(ratelimit.LoginIPEmail.Prefix + key))
	if len(stored) != 32 || !bytes.Equal(stored, want[:]) {
		t.Errorf("stored key = %x, want sha256(prefix+key) = %x", stored, want)
	}
	if bytes.Contains(stored, []byte("ana@example.com")) || bytes.Contains(stored, []byte("203.0.113.9")) {
		t.Error("stored key contains the clear email or IP")
	}
	if got := ratelimit.HashKey(ratelimit.LoginIPEmail, key); !bytes.Equal(got, want[:]) {
		t.Errorf("HashKey = %x, want %x", got, want)
	}
}

func TestPostgres_RulesDoNotShareBuckets(t *testing.T) {
	l, _, _ := newPostgres(t)
	for range 3 {
		if _, err := l.Allow(t.Context(), ratelimit.RegisterIP, "1.2.3.4"); err != nil {
			t.Fatal(err)
		}
	}
	// The same raw key under another rule prefix is a different bucket.
	if d, err := l.Allow(t.Context(), ratelimit.CodeRequestIP, "1.2.3.4"); err != nil || !d.Allowed {
		t.Errorf("other rule = %+v, %v; want allowed", d, err)
	}
}

func TestPostgres_StoreErrorIsReturned(t *testing.T) {
	db := testsupport.NewTestDB(t)
	pool, err := pgxpool.New(t.Context(), db.AppURL)
	if err != nil {
		t.Fatal(err)
	}
	pool.Close() // a closed pool fails every query
	l := ratelimit.NewPostgres(pool, testsupport.NewFakeClock(start))
	if _, err := l.Allow(t.Context(), ratelimit.LoginIPEmail, "k"); err == nil {
		t.Fatal("Allow on a closed pool returned a nil error")
	}
}

func TestPurge_RemovesExpired(t *testing.T) {
	l, clk, db := newPostgres(t)
	if _, err := l.Allow(t.Context(), ratelimit.LoginIPEmail, "old"); err != nil { // expires 12:01:00
		t.Fatal(err)
	}
	clk.Advance(90 * time.Second)                                                    // 12:01:40
	if _, err := l.Allow(t.Context(), ratelimit.LoginIPEmail, "fresh"); err != nil { // expires 12:02:00
		t.Fatal(err)
	}
	n, err := ratelimit.Purge(t.Context(), db.App, clk.Now())
	if err != nil || n != 1 {
		t.Fatalf("Purge = %d, %v; want 1 row", n, err)
	}
	var left int
	if err := db.Super.QueryRow(t.Context(), `select count(*) from rate_limit_counters`).Scan(&left); err != nil || left != 1 {
		t.Fatalf("remaining rows = %d, %v; want 1", left, err)
	}
}
