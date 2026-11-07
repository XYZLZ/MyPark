package auth_test

import (
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
)

const purgeDay = 24 * time.Hour

func TestAuthPurge_RemovesOnlyDataPast30Days(t *testing.T) {
	e := newEnv(t)
	u := e.insertUser("ana@example.com", "driver", "active", true)
	now := e.clock.Now()

	// Codes: purge only expired > 30 days. All consumed so they can coexist.
	for _, c := range []struct {
		purpose string
		expires time.Time
	}{
		{"verify_email", now.Add(-31 * purgeDay)},
		{"reset_password", now.Add(-29 * purgeDay)},
		{"admin_invite", now.Add(time.Hour)},
	} {
		e.exec(`insert into auth_codes (user_id, purpose, code_hash, expires_at, consumed_at)
			values ($1, $2, '\x00', $3, $4)`, u.ID, c.purpose, c.expires, now)
	}

	// Sessions: old revoked, old expired, recently revoked, live.
	type sess struct {
		name    string
		revoked *time.Time
		expires time.Time
	}
	old, recent := now.Add(-31*purgeDay), now.Add(-29*purgeDay)
	sessions := []sess{
		{"revoked-old", &old, now.Add(60 * purgeDay)},
		{"expired-old", nil, now.Add(-31 * purgeDay)},
		{"revoked-recent", &recent, now.Add(60 * purgeDay)},
		{"live", nil, now.Add(60 * purgeDay)},
	}
	ids := map[string]int64{}
	for _, s := range sessions {
		var reason any
		if s.revoked != nil {
			reason = "logout"
		}
		var id int64
		if err := e.db.Super.QueryRow(t.Context(), `insert into auth_sessions (user_id, client_kind, absolute_expires_at, revoked_at, revoked_reason, user_agent)
			values ($1, 'web', $2, $3, $4, $5) returning id`, u.ID, s.expires, s.revoked, reason, s.name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[s.name] = id
		// A live token of every session, plus an expired-old token on the live one.
		e.exec(`insert into refresh_tokens (session_id, token_hash, expires_at) values ($1, $2, $3)`,
			id, []byte(s.name), now.Add(10*purgeDay))
	}
	e.exec(`insert into refresh_tokens (session_id, token_hash, expires_at) values ($1, '\x01', $2), ($1, '\x02', $3)`,
		ids["live"], now.Add(-31*purgeDay), now.Add(-29*purgeDay))

	p := auth.NewPurger(e.db.App, e.clock)
	if err := p.Purge(t.Context()); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	if got := e.text(`select string_agg(purpose, ',' order by purpose) from auth_codes`); got != "admin_invite,reset_password" {
		t.Errorf("codes left = %s", got)
	}
	if got := e.text(`select string_agg(user_agent, ',' order by user_agent) from auth_sessions`); got != "live,revoked-recent" {
		t.Errorf("sessions left = %s", got)
	}
	// Tokens: live and revoked-recent keep their live token; live keeps the 29-day-old one.
	if got := e.count(`select count(*) from refresh_tokens`); got != 3 {
		t.Errorf("refresh tokens left = %d, want 3 (live, revoked-recent, 29d-expired)", got)
	}
	if got := e.count(`select count(*) from refresh_tokens where token_hash = '\x01'`); got != 0 {
		t.Error("token expired 31 days ago was kept")
	}

	// Idempotent, and the cut-off follows the injected clock.
	if err := p.Purge(t.Context()); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(3 * purgeDay)
	if err := p.Purge(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := e.count(`select count(*) from auth_codes`); got != 1 {
		t.Errorf("after advancing the clock codes left = %d, want 1", got)
	}
	if got := e.text(`select string_agg(user_agent, ',') from auth_sessions`); got != "live" {
		t.Errorf("after advancing the clock sessions left = %s, want live", got)
	}
}
