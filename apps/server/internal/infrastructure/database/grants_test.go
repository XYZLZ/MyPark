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

	sqlStateInsufficientPrivilege = "42501"
)

func TestGrants_AppDMLOnIdentityTables(t *testing.T) {
	db := testsupport.NewTestDB(t)
	// Each privilege is checked on its own: has_table_privilege with a list
	// is true when ANY privilege is held.
	for _, table := range []string{"users", "auth_sessions", "refresh_tokens", "auth_codes", "legal_documents", "vehicles", "rate_limit_counters"} {
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

func TestGrants_AuditLogAppendOnly(t *testing.T) {
	db := testsupport.NewTestDB(t)
	// Catalog check, one privilege per call (a list is true when ANY is held).
	for priv, want := range map[string]bool{
		"SELECT": true, "INSERT": true, "UPDATE": false, "DELETE": false, "TRUNCATE": false,
	} {
		var got bool
		if err := db.Super.QueryRow(t.Context(),
			"select has_table_privilege('mypark_app', 'audit_log', $1)", priv).Scan(&got); err != nil {
			t.Fatalf("has_table_privilege(%s): %v", priv, err)
		}
		if got != want {
			t.Errorf("mypark_app %s on audit_log = %v, want %v", priv, got, want)
		}
	}
	// Behavioral check as the application role.
	if got := execCode(t, db.App, `insert into audit_log (actor_kind, action) values ('system', 'admin.seeded')`); got != "" {
		t.Fatalf("insert SQLSTATE = %q, want success", got)
	}
	var n int
	if err := db.App.QueryRow(t.Context(), `select count(*) from audit_log`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("select as app: n=%d err=%v", n, err)
	}
	for _, stmt := range []string{
		`update audit_log set action = 'x'`,
		`delete from audit_log`,
		`truncate audit_log`,
	} {
		if got := execCode(t, db.App, stmt); got != sqlStateInsufficientPrivilege {
			t.Errorf("%q SQLSTATE = %q, want %s", stmt, got, sqlStateInsufficientPrivilege)
		}
	}
}

func TestGrants_AuditLogChecks(t *testing.T) {
	db := testsupport.NewTestDB(t)
	if got := execCode(t, db.App, `insert into audit_log (actor_kind, action) values ('root', 'x')`); got != sqlStateCheckViolation {
		t.Errorf("bad actor_kind SQLSTATE = %q, want %s", got, sqlStateCheckViolation)
	}
}

func TestGrants_AppDMLOnOutbox(t *testing.T) {
	db := testsupport.NewTestDB(t)
	for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
		var ok bool
		if err := db.Super.QueryRow(t.Context(),
			"select has_table_privilege('mypark_app', 'outbox_messages', $1)", priv).Scan(&ok); err != nil {
			t.Fatalf("has_table_privilege(%s): %v", priv, err)
		}
		if !ok {
			t.Errorf("mypark_app lacks %s on outbox_messages", priv)
		}
	}
	const ins = `insert into outbox_messages (kind, template, locale, to_address, payload, dedupe_key)
		values ('email', $1, $2, 'a@x.do', '{}', $3)`
	cases := []struct {
		name, tpl, locale, key, want string
	}{
		{"valid", "verify_email", "es-DO", "k1", ""},
		{"duplicate dedupe key", "verify_email", "es-DO", "k1", sqlStateUniqueViolation},
		{"unknown template", "newsletter", "es-DO", "k2", sqlStateCheckViolation},
		{"unknown locale", "verify_email", "fr", "k3", sqlStateCheckViolation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := execCode(t, db.App, ins, tc.tpl, tc.locale, tc.key); got != tc.want {
				t.Errorf("SQLSTATE = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestVehicles_ChecksAndActivePlateUniqueness(t *testing.T) {
	pool := testsupport.NewDB(t)
	uid := newUserID(t, pool, "a@x.do")
	const ins = `insert into vehicles (user_id, plate_normalized, plate_display, make, model, color)
		values ($1, $2, $3, $4, 'Corolla', 'Gris')`
	for name, tc := range map[string]struct {
		norm, disp, make string
		want             string
	}{
		"lowercase normalized": {"abc123", "abc123", "Toyota", sqlStateCheckViolation},
		"one char":             {"A", "A1", "Toyota", sqlStateCheckViolation},
		"eleven chars":         {"ABCDEFGHIJK", "ABCDEFGHIJK", "Toyota", sqlStateCheckViolation},
		"empty make":           {"ABC123", "ABC123", "", sqlStateCheckViolation},
		"display too long":     {"ABC123", strings.Repeat("-", 16), "Toyota", sqlStateCheckViolation},
		"valid":                {"ABC123", "abc-123", "Toyota", ""},
		"duplicate active":     {"ABC123", "ABC 123", "Toyota", sqlStateUniqueViolation},
	} {
		if got := execCode(t, pool, ins, uid, tc.norm, tc.disp, tc.make); got != tc.want {
			t.Errorf("%s: SQLSTATE = %q, want %q", name, got, tc.want)
		}
	}
	if got := execCode(t, pool, `update vehicles set deleted_at = now() where plate_normalized = 'ABC123'`); got != "" {
		t.Fatalf("soft delete SQLSTATE = %q", got)
	}
	if got := execCode(t, pool, ins, uid, "ABC123", "ABC123", "Toyota"); got != "" {
		t.Errorf("reuse after soft delete SQLSTATE = %q, want success", got)
	}
}

func TestRateLimitCounters_UnloggedWithPurgeIndex(t *testing.T) {
	db := testsupport.NewTestDB(t)
	var persistence string
	if err := db.Super.QueryRow(t.Context(),
		`select relpersistence::text from pg_class where relname = 'rate_limit_counters'`).Scan(&persistence); err != nil {
		t.Fatalf("relpersistence: %v", err)
	}
	if persistence != "u" {
		t.Errorf("rate_limit_counters persistence = %q, want u (unlogged)", persistence)
	}
	var indexed bool
	if err := db.Super.QueryRow(t.Context(),
		`select exists (select 1 from pg_indexes where tablename = 'rate_limit_counters' and indexdef like '%(expires_at)%')`).Scan(&indexed); err != nil || !indexed {
		t.Errorf("expires_at index present = %v, %v; want true", indexed, err)
	}
}
