package auth_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	authpg "github.com/XYZLZ/MyPark/apps/server/internal/features/auth/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/jwt"
)

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

// wantAppErr asserts err is an apperr with this kind and code.
func wantAppErr(t *testing.T, err error, kind apperr.Kind, code errcodes.Code) {
	t.Helper()
	ae, ok := errors.AsType[*apperr.Error](err)
	if !ok {
		t.Fatalf("err = %v, want *apperr.Error %s", err, code)
	}
	if ae.Kind != kind || ae.Code != code {
		t.Errorf("err = kind %d code %s, want kind %d code %s", ae.Kind, ae.Code, kind, code)
	}
}

func TestAuthenticate_Table(t *testing.T) {
	const email = "ana@example.com"
	const dayAfterLogin = 91 * 24 * time.Hour

	type setup func(e *env, u userInfo, access string) string
	sessionOf := func(e *env, access string) uuid.UUID { return e.claims(access).SessionID }

	rows := []struct {
		name  string
		setup setup
		kind  apperr.Kind
		code  errcodes.Code // empty: success
	}{
		{"valid", func(_ *env, _ userInfo, access string) string { return access }, 0, ""},
		{"garbage", func(*env, userInfo, string) string { return "not-a-jwt" }, apperr.KindUnauthenticated, errcodes.AuthUnauthenticated},
		{"empty", func(*env, userInfo, string) string { return "" }, apperr.KindUnauthenticated, errcodes.AuthUnauthenticated},
		{"expired token", func(e *env, _ userInfo, access string) string {
			e.clock.Advance(jwt.AccessTTL + time.Minute)
			return access
		}, apperr.KindUnauthenticated, errcodes.AuthUnauthenticated},
		{"alg none", func(e *env, u userInfo, access string) string {
			c := e.claims(access)
			payload := fmt.Sprintf(`{"iss":%q,"aud":%q,"sub":%q,"sid":%q,"role":"driver","exp":%d}`,
				jwt.Issuer, jwt.Audience, u.PublicID, c.SessionID, e.clock.Now().Add(time.Hour).Unix())
			return b64(`{"alg":"none","typ":"JWT","kid":"test"}`) + "." + b64(payload) + "."
		}, apperr.KindUnauthenticated, errcodes.AuthUnauthenticated},
		{"unknown kid", func(e *env, u userInfo, access string) string {
			other, err := jwt.NewSigner("other", jwtSeed, e.clock, jwt.AccessTTL)
			if err != nil {
				e.t.Fatal(err)
			}
			tok, _, err := other.Sign(u.PublicID, sessionOf(e, access), authz.RoleDriver)
			if err != nil {
				e.t.Fatal(err)
			}
			return tok
		}, apperr.KindUnauthenticated, errcodes.AuthUnauthenticated},
		{"unknown sid", func(e *env, u userInfo, _ string) string {
			tok, _, err := e.signer.Sign(u.PublicID, uuid.New(), authz.RoleDriver)
			if err != nil {
				e.t.Fatal(err)
			}
			return tok
		}, apperr.KindUnauthenticated, errcodes.AuthUnauthenticated},
		{"sid of another user", func(e *env, _ userInfo, access string) string {
			tok, _, err := e.signer.Sign(uuid.New(), sessionOf(e, access), authz.RoleDriver)
			if err != nil {
				e.t.Fatal(err)
			}
			return tok
		}, apperr.KindUnauthenticated, errcodes.AuthUnauthenticated},
		{"revoked session", func(e *env, _ userInfo, access string) string {
			e.exec(`update auth_sessions set revoked_at = now(), revoked_reason = 'logout' where public_id = $1`, sessionOf(e, access))
			return access
		}, apperr.KindUnauthenticated, errcodes.AuthSessionRevoked},
		{"past absolute expiry", func(e *env, u userInfo, access string) string {
			sid := sessionOf(e, access)
			e.clock.Advance(dayAfterLogin)
			tok, _, err := e.signer.Sign(u.PublicID, sid, authz.RoleDriver)
			if err != nil {
				e.t.Fatal(err)
			}
			return tok
		}, apperr.KindUnauthenticated, errcodes.AuthSessionRevoked},
		{"suspended", func(e *env, u userInfo, access string) string {
			e.exec(`update users set status = 'suspended' where id = $1`, u.ID)
			return access
		}, apperr.KindForbidden, errcodes.AuthAccountSuspended},
		{"deleted", func(e *env, u userInfo, access string) string {
			e.exec(`update users set status = 'deleted', deleted_at = now() where id = $1`, u.ID)
			return access
		}, apperr.KindUnauthenticated, errcodes.AuthSessionRevoked},
		{"pending verification", func(e *env, u userInfo, access string) string {
			e.exec(`update users set status = 'pending_verification' where id = $1`, u.ID)
			return access
		}, apperr.KindUnauthenticated, errcodes.AuthSessionRevoked},
		{"db role admin, claim driver", func(e *env, u userInfo, access string) string {
			e.exec(`update users set role = 'admin' where id = $1`, u.ID)
			return access
		}, apperr.KindUnauthenticated, errcodes.AuthSessionRevoked},
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			u := e.activeDriver(email)
			access := e.loginAccess(email)
			token := tc.setup(e, u, access)
			p, err := e.authenticator.Authenticate(t.Context(), token)
			if tc.code != "" {
				wantAppErr(t, err, tc.kind, tc.code)
				return
			}
			if err != nil {
				t.Fatalf("Authenticate: %v", err)
			}
			want := authz.Principal{
				UserID: u.ID, UserPublicID: u.PublicID, Role: authz.RoleDriver,
				SessionID: p.SessionID, SessionPublicID: e.claims(access).SessionID, LegalPending: false,
			}
			if p != want || p.SessionID == 0 {
				t.Errorf("principal = %+v, want %+v", p, want)
			}
		})
	}
}

func TestAuthenticate_LegalPendingDriversOnly(t *testing.T) {
	e := newEnv(t)
	outdated := e.activeDriver("old@example.com")
	e.exec(`update users set terms_version = '2025-01-01' where id = $1`, outdated.ID)
	e.activeDriver("new@example.com")
	// Null versions: a driver who never accepted, and an admin, who is exempt.
	e.insertUser("none@example.com", "driver", "active", true)
	e.insertUser("admin@example.com", "admin", "active", true)

	for _, tc := range []struct {
		email   string
		pending bool
	}{
		{"old@example.com", true},
		{"new@example.com", false},
		{"none@example.com", true},
		{"admin@example.com", false},
	} {
		p, err := e.authenticator.Authenticate(t.Context(), e.loginAccess(tc.email))
		if err != nil {
			t.Fatalf("%s: %v", tc.email, err)
		}
		if p.LegalPending != tc.pending {
			t.Errorf("%s: LegalPending = %v, want %v", tc.email, p.LegalPending, tc.pending)
		}
	}
}

// countingRepo counts GetSessionPrincipal calls.
type countingRepo struct {
	auth.Repo
	calls atomic.Int64
}

func (r *countingRepo) GetSessionPrincipal(ctx context.Context, sid, sub uuid.UUID, now time.Time) (auth.SessionPrincipal, bool, error) {
	r.calls.Add(1)
	return r.Repo.GetSessionPrincipal(ctx, sid, sub, now)
}

func TestAuthenticate_OneSessionQueryPerRequest(t *testing.T) {
	e := newEnv(t)
	e.activeDriver("ana@example.com")
	access := e.loginAccess("ana@example.com")
	repo := &countingRepo{Repo: authpg.NewRepo(e.db.App)}
	a := auth.NewAuthenticator(e.verifier, repo, e.legal, e.clock)
	if _, err := a.Authenticate(t.Context(), access); err != nil {
		t.Fatal(err)
	}
	if n := repo.calls.Load(); n != 1 {
		t.Errorf("GetSessionPrincipal calls = %d, want 1", n)
	}
}

func TestAuthenticate_NeverTouchesTheDatabaseForABadToken(t *testing.T) {
	e := newEnv(t)
	repo := &countingRepo{Repo: authpg.NewRepo(e.db.App)}
	a := auth.NewAuthenticator(e.verifier, repo, e.legal, e.clock)
	if _, err := a.Authenticate(t.Context(), "garbage"); err == nil {
		t.Fatal("garbage token authenticated")
	}
	if n := repo.calls.Load(); n != 0 {
		t.Errorf("GetSessionPrincipal calls = %d, want 0 before the signature is checked", n)
	}
}
