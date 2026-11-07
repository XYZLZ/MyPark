package database_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

const (
	sqlStateCheckViolation  = "23514"
	sqlStateUniqueViolation = "23505"
)

func TestGrants_AppDMLOnIdentityTables(t *testing.T) {
	db := testsupport.NewTestDB(t)
	// Each privilege is checked on its own: has_table_privilege with a list
	// is true when ANY privilege is held.
	for _, table := range []string{"users", "auth_sessions", "refresh_tokens", "auth_codes", "legal_documents"} {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			var ok bool
			err := db.Super.QueryRow(t.Context(),
				"select has_table_privilege('mypark_app', $1, $2)", table, priv).Scan(&ok)
			if err != nil {
				t.Fatalf("has_table_privilege(%s, %s): %v", table, priv, err)
			}
			if !ok {
				t.Errorf("mypark_app lacks %s on %s", priv, table)
			}
		}
	}
}

func execCode(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	_, err := pool.Exec(t.Context(), sql, args...)
	if err == nil {
		return ""
	}
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		t.Fatalf("exec %q: non-postgres error: %v", sql, err)
	}
	return pgErr.Code
}

const insertUser = `insert into users (email, first_name, last_name, phone, role, status, locale, deleted_at)
	values ($1, $2, $3, $4, $5, $6, $7, $8)`

func TestUsers_ChecksRejectBadRows(t *testing.T) {
	pool := testsupport.NewDB(t)
	long := strings.Repeat("x", 81)
	cases := []struct {
		name                                     string
		first, last, phone, role, status, locale any
		deletedAt                                any
	}{
		{"role root", "Ana", "Perez", nil, "root", "active", "es-DO", nil},
		{"status gone", "Ana", "Perez", nil, "driver", "gone", "es-DO", nil},
		{"locale fr", "Ana", "Perez", nil, "driver", "active", "fr", nil},
		{"empty first_name", "", "Perez", nil, "driver", "active", "es-DO", nil},
		{"last_name 81 chars", "Ana", long, nil, "driver", "active", "es-DO", nil},
		{"short phone", "Ana", "Perez", "809555", "driver", "active", "es-DO", nil},
		{"deleted without deleted_at", "Ana", "Perez", nil, "driver", "deleted", "es-DO", nil},
		{"deleted_at without deleted status", "Ana", "Perez", nil, "driver", "active", "es-DO", "2026-11-07T00:00:00Z"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			email := "u" + string(rune('a'+i)) + "@x.do"
			got := execCode(t, pool, insertUser, email, tc.first, tc.last, tc.phone, tc.role, tc.status, tc.locale, tc.deletedAt)
			if got != sqlStateCheckViolation {
				t.Errorf("SQLSTATE = %q, want %s", got, sqlStateCheckViolation)
			}
		})
	}
	t.Run("valid row is accepted", func(t *testing.T) {
		if got := execCode(t, pool, insertUser, "ok@x.do", "Ana", "Perez Gomez", "+18095550100", "driver", "active", "es-DO", nil); got != "" {
			t.Errorf("SQLSTATE = %q, want success", got)
		}
	})
}

func TestUsers_EmailUniqueAmongNonDeletedCaseInsensitive(t *testing.T) {
	pool := testsupport.NewDB(t)
	if got := execCode(t, pool, insertUser, "Ana@x.do", "Ana", "P", nil, "driver", "active", "es-DO", nil); got != "" {
		t.Fatalf("first insert SQLSTATE = %q", got)
	}
	if got := execCode(t, pool, insertUser, "ana@x.do", "Ana", "P", nil, "driver", "active", "es-DO", nil); got != sqlStateUniqueViolation {
		t.Fatalf("duplicate insert SQLSTATE = %q, want %s", got, sqlStateUniqueViolation)
	}
	if got := execCode(t, pool, `update users set status = 'deleted', deleted_at = now() where email = 'ana@x.do'`); got != "" {
		t.Fatalf("soft delete SQLSTATE = %q", got)
	}
	if got := execCode(t, pool, insertUser, "ana@x.do", "Ana", "P", nil, "driver", "active", "es-DO", nil); got != "" {
		t.Errorf("insert after soft delete SQLSTATE = %q, want success", got)
	}
}

func newUserID(t *testing.T, pool *pgxpool.Pool, email string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(t.Context(),
		`insert into users (email, first_name, last_name) values ($1, 'Ana', 'P') returning id`, email).Scan(&id)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func TestAuthCodes_OneLivePerPurpose(t *testing.T) {
	pool := testsupport.NewDB(t)
	uid := newUserID(t, pool, "a@x.do")
	const ins = `insert into auth_codes (user_id, purpose, code_hash, expires_at)
		values ($1, $2, '\x01', now() + interval '15 minutes')`
	if got := execCode(t, pool, ins, uid, "verify_email"); got != "" {
		t.Fatalf("first code SQLSTATE = %q", got)
	}
	if got := execCode(t, pool, ins, uid, "verify_email"); got != sqlStateUniqueViolation {
		t.Errorf("second live code SQLSTATE = %q, want %s", got, sqlStateUniqueViolation)
	}
	if got := execCode(t, pool, ins, uid, "reset_password"); got != "" {
		t.Errorf("other purpose SQLSTATE = %q, want success", got)
	}
	if got := execCode(t, pool, `update auth_codes set consumed_at = now() where user_id = $1 and purpose = 'verify_email'`, uid); got != "" {
		t.Fatalf("consume SQLSTATE = %q", got)
	}
	if got := execCode(t, pool, ins, uid, "verify_email"); got != "" {
		t.Errorf("code after consume SQLSTATE = %q, want success", got)
	}
	if got := execCode(t, pool, `update auth_codes set attempts = 6 where purpose = 'reset_password'`); got != sqlStateCheckViolation {
		t.Errorf("attempts 6 SQLSTATE = %q, want %s", got, sqlStateCheckViolation)
	}
}

func TestAuthSessions_RevokedReasonPairing(t *testing.T) {
	pool := testsupport.NewDB(t)
	uid := newUserID(t, pool, "a@x.do")
	const ins = `insert into auth_sessions (user_id, client_kind, absolute_expires_at, revoked_at, revoked_reason)
		values ($1, $2, now() + interval '90 days', $3, $4)`
	cases := []struct {
		name     string
		kind     string
		at       any
		reason   any
		wantCode string
	}{
		{"revoked_at without reason", "web", "2026-11-07T00:00:00Z", nil, sqlStateCheckViolation},
		{"reason without revoked_at", "web", nil, "logout", sqlStateCheckViolation},
		{"unknown reason", "web", "2026-11-07T00:00:00Z", "bored", sqlStateCheckViolation},
		{"unknown client kind", "desktop", nil, nil, sqlStateCheckViolation},
		{"paired", "mobile", "2026-11-07T00:00:00Z", "logout_all", ""},
		{"live", "web", nil, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := execCode(t, pool, ins, uid, tc.kind, tc.at, tc.reason); got != tc.wantCode {
				t.Errorf("SQLSTATE = %q, want %q", got, tc.wantCode)
			}
		})
	}
}
