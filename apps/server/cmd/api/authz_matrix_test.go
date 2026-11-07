package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// matrixEntry says how to call one route. build returns a request whose body
// and path are valid for the route; the runner adds the bearer token. owned
// marks routes addressed at a resource of the owner (vehicles/{id}).
type matrixEntry struct {
	build func(f *fixture) *http.Request
	owned bool
}

func vehiclePath(f *fixture) string { return "/v1/vehicles/" + f.vehicleID.String() }

// matrix has one entry per route pattern of the registry. TestAuthzMatrix
// fails for a registered route that is missing here.
var matrix = map[string]matrixEntry{
	"GET /v1/legal/current": {build: func(*fixture) *http.Request { return jsonReq("GET", "/v1/legal/current", nil) }},

	"POST /v1/auth/register": {build: func(f *fixture) *http.Request {
		return jsonReq("POST", "/v1/auth/register", map[string]any{
			"email": f.email(), "password": "Zx9!kelp-orbit", "first_name": "Ana", "last_name": "Perez",
			"accepted_terms_version": legal.InitialVersion, "accepted_privacy_version": legal.InitialVersion,
		})
	}},
	"POST /v1/auth/verify": {build: func(f *fixture) *http.Request {
		return jsonReq("POST", "/v1/auth/verify", map[string]any{"email": f.email(), "code": "123456"})
	}},
	"POST /v1/auth/verify/resend": {build: func(f *fixture) *http.Request {
		return jsonReq("POST", "/v1/auth/verify/resend", map[string]any{"email": f.email()})
	}},
	"POST /v1/auth/password/forgot": {build: func(f *fixture) *http.Request {
		return jsonReq("POST", "/v1/auth/password/forgot", map[string]any{"email": f.email()})
	}},
	"POST /v1/auth/password/reset": {build: func(f *fixture) *http.Request {
		return jsonReq("POST", "/v1/auth/password/reset",
			map[string]any{"email": f.email(), "code": "123456", "new_password": "Qw8#plum-harbor"})
	}},
	"POST /v1/auth/invitations/accept": {build: func(f *fixture) *http.Request {
		return jsonReq("POST", "/v1/auth/invitations/accept",
			map[string]any{"email": f.email(), "code": "123456", "password": "Qw8#plum-harbor"})
	}},
	"POST /v1/auth/login": {build: func(f *fixture) *http.Request {
		return jsonReq("POST", "/v1/auth/login",
			map[string]any{"email": f.email(), "password": "Zx9!kelp-orbit", "client_kind": "web"})
	}},
	"POST /v1/auth/refresh": {build: func(*fixture) *http.Request {
		return jsonReq("POST", "/v1/auth/refresh", map[string]any{"refresh_token": "not-a-real-token"})
	}},
	"POST /v1/auth/logout":     {build: func(*fixture) *http.Request { return jsonReq("POST", "/v1/auth/logout", nil) }},
	"POST /v1/auth/logout-all": {build: func(*fixture) *http.Request { return jsonReq("POST", "/v1/auth/logout-all", nil) }},

	"GET /v1/me": {build: func(*fixture) *http.Request { return jsonReq("GET", "/v1/me", nil) }},
	"PATCH /v1/me": {build: func(*fixture) *http.Request {
		return jsonReq("PATCH", "/v1/me", map[string]any{"first_name": "Renamed"})
	}},
	"DELETE /v1/me": {build: func(*fixture) *http.Request {
		return jsonReq("DELETE", "/v1/me", map[string]any{"password": fixturePassword})
	}},
	"POST /v1/me/legal-acceptance": {build: func(*fixture) *http.Request {
		return jsonReq("POST", "/v1/me/legal-acceptance",
			map[string]any{"terms_version": legal.InitialVersion, "privacy_version": legal.InitialVersion})
	}},

	"GET /v1/vehicles": {build: func(*fixture) *http.Request { return jsonReq("GET", "/v1/vehicles", nil) }},
	"POST /v1/vehicles": {build: func(f *fixture) *http.Request {
		return jsonReq("POST", "/v1/vehicles", map[string]any{
			"plate": "NEW" + uuid.NewString()[:6], "make": "Honda", "model": "Civic", "color": "Red",
		})
	}},
	"GET /v1/vehicles/{id}": {owned: true, build: func(f *fixture) *http.Request {
		return jsonReq("GET", vehiclePath(f), nil)
	}},
	"PATCH /v1/vehicles/{id}": {owned: true, build: func(f *fixture) *http.Request {
		return jsonReq("PATCH", vehiclePath(f), map[string]any{"color": "Green"})
	}},
	"DELETE /v1/vehicles/{id}": {owned: true, build: func(f *fixture) *http.Request {
		return jsonReq("DELETE", vehiclePath(f), nil)
	}},

	"GET /v1/admin/admins": {build: func(*fixture) *http.Request { return jsonReq("GET", "/v1/admin/admins", nil) }},
	"POST /v1/admin/admins": {build: func(f *fixture) *http.Request {
		return jsonReq("POST", "/v1/admin/admins",
			map[string]any{"email": f.email(), "first_name": "New", "last_name": "Admin"})
	}},
}

// gateCodes are the codes only the access gates produce. A request that
// passed the gates must not answer with one of them.
var gateCodes = []string{
	string(errcodes.AuthUnauthenticated), string(errcodes.AuthSessionRevoked),
	string(errcodes.AuthForbidden), string(errcodes.AuthAccountSuspended),
	string(errcodes.LegalAcceptanceRequired),
}

// outcome is what the policy of a route requires for one principal.
type outcome struct {
	status int    // 0 means: passes the gates (handler decides)
	code   string // required when status != 0
}

// row is one principal of the matrix.
type row struct {
	name  string
	token func(f *fixture) string
	role  authz.Role // role the token carries, "" for no usable identity
	kind  string     // anonymous | bad-token | revoked | suspended | active
	legal bool       // owes legal re-acceptance
}

var rows = []row{
	{name: "anonymous", token: func(*fixture) string { return "" }, kind: "anonymous"},
	{name: "tampered", token: func(f *fixture) string { return f.tampered }, kind: "bad-token"},
	{name: "expired", token: func(f *fixture) string { return f.expired }, kind: "bad-token"},
	{name: "alg-none", token: func(f *fixture) string { return f.algNone }, kind: "bad-token"},
	{name: "revoked-session", token: func(f *fixture) string { return f.revoked.token }, kind: "revoked"},
	{name: "suspended", token: func(f *fixture) string { return f.suspended.token }, kind: "suspended"},
	{name: "legal-pending-driver", token: func(f *fixture) string { return f.pending.token }, role: authz.RoleDriver, kind: "active", legal: true},
	{name: "admin", token: func(f *fixture) string { return f.admin.token }, role: authz.RoleAdmin, kind: "active"},
	{name: "other-driver", token: func(f *fixture) string { return f.other.token }, role: authz.RoleDriver, kind: "active"},
	// The owner goes last: owned deletes and logouts change its state.
	{name: "owner-driver", token: func(f *fixture) string { return f.owner.token }, role: authz.RoleDriver, kind: "active"},
}

// expected derives the outcome from the route policy (spec section 8).
func expected(p router.Policy, r row) outcome {
	if p.Public {
		return outcome{}
	}
	switch r.kind {
	case "anonymous", "bad-token":
		return outcome{http.StatusUnauthorized, string(errcodes.AuthUnauthenticated)}
	case "revoked":
		return outcome{http.StatusUnauthorized, string(errcodes.AuthSessionRevoked)}
	case "suspended":
		return outcome{http.StatusForbidden, string(errcodes.AuthAccountSuspended)}
	}
	if !slices.Contains(p.Roles, r.role) {
		return outcome{http.StatusForbidden, string(errcodes.AuthForbidden)}
	}
	if r.legal && !p.LegalExempt {
		return outcome{http.StatusForbidden, string(errcodes.LegalAcceptanceRequired)}
	}
	return outcome{}
}

// Spec 15.5: the matrix is generated from the route registry, so a new route
// without an entry fails here.
func TestAuthzMatrix(t *testing.T) {
	e := newAPIEnv(t)
	for _, rt := range e.routes {
		if _, ok := matrix[rt.Pattern]; !ok {
			t.Errorf("route %s has no matrix entry", rt.Pattern)
		}
	}
	registered := map[string]bool{}
	for _, rt := range e.routes {
		registered[rt.Pattern] = true
	}
	for pattern := range matrix {
		if !registered[pattern] {
			t.Errorf("matrix entry %q is not a registered route", pattern)
		}
	}
	if t.Failed() {
		return
	}

	for _, rt := range e.routes {
		entry := matrix[rt.Pattern]
		t.Run(rt.Pattern, func(t *testing.T) {
			f := e.newFixture()
			for _, r := range rows {
				want := expected(rt.Policy, r)
				if entry.owned && r.name == "other-driver" {
					// Not yours and not there look the same (spec section 8).
					want = outcome{http.StatusNotFound, string(errcodes.VehicleNotFound)}
				}
				rec := e.serve(entry.build(f), r.token(f))
				body := decodeBody(t, rec)
				if want.status != 0 {
					if rec.Code != want.status || errCode(body) != want.code {
						t.Errorf("%s: got %d %q, want %d %q (body %s)",
							r.name, rec.Code, errCode(body), want.status, want.code, rec.Body)
					}
					if entry.owned && r.name == "other-driver" {
						assertSameAsMissing(t, e, f, entry, rec)
					}
					continue
				}
				// Passed the gates: the handler decides, but never with a gate code.
				if slices.Contains(gateCodes, errCode(body)) || rec.Code >= http.StatusInternalServerError ||
					rec.Code == http.StatusNotFound && errCode(body) == string(errcodes.HTTPNotFound) {
					t.Errorf("%s: got %d %q, want the request to reach the handler (body %s)",
						r.name, rec.Code, errCode(body), rec.Body)
				}
			}
		})
	}
}

// assertSameAsMissing checks that another driver's 404 equals the 404 for an
// id that does not exist, apart from meta.request_id.
func assertSameAsMissing(t *testing.T, e *apiEnv, f *fixture, entry matrixEntry, got *httptest.ResponseRecorder) {
	t.Helper()
	missing := *f
	missing.vehicleID = uuid.New()
	rec := e.serve(entry.build(&missing), f.owner.token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing resource: got %d %s, want 404", rec.Code, rec.Body)
	}
	strip := func(r *httptest.ResponseRecorder) map[string]any {
		b := decodeBody(t, r)
		delete(b["meta"].(map[string]any), "request_id")
		return b
	}
	if !reflect.DeepEqual(strip(got), strip(rec)) {
		t.Errorf("not-owned 404 %s differs from missing 404 %s", got.Body, rec.Body)
	}
	if got.Code != rec.Code {
		t.Errorf("not-owned status %d differs from missing %d", got.Code, rec.Code)
	}
}

// Any future direct registration on the api mux must show up in the registry
// or in the public allow-list below (A7 ruling). ServeMux cannot list its
// patterns, so this scans the production sources for registrations that skip
// router.Router, and checks the live mux for the registered ones.
func TestEveryServedPatternIsRegisteredOrAllowListed(t *testing.T) {
	e := newAPIEnv(t)

	allowList := map[string]bool{"GET /healthz": true, "GET /readyz": true}
	served := map[string]bool{}
	for p := range allowList {
		served[p] = true
	}
	for _, rt := range e.routes {
		served[rt.Pattern] = true
	}
	// The live mux resolves every served pattern to itself.
	for p := range served {
		method, path, _ := strings.Cut(p, " ")
		path = strings.ReplaceAll(path, "{id}", uuid.NewString())
		req := httptest.NewRequest(method, path, nil)
		rec := e.serve(req, "")
		if rec.Code == http.StatusNotFound && errCode(decodeBody(t, rec)) == string(errcodes.HTTPNotFound) {
			t.Errorf("pattern %q is not served by the mux", p)
		}
	}

	// Direct registrations on a ServeMux are allowed only in the router and
	// in the health feature (the allow-list above).
	root := filepath.Join("..", "..")
	allowedFiles := map[string]bool{
		filepath.Join("internal", "transport", "http", "router", "router.go"): true,
		filepath.Join("internal", "features", "health", "handler.go"):         true,
	}
	var found []string
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, de os.DirEntry, err error) error {
			if err != nil || de.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
					return true
				}
				if len(call.Args) != 2 || !isMuxReceiver(sel.X) {
					return true
				}
				if !allowedFiles[rel] {
					found = append(found, rel+": "+sel.Sel.Name)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", dir, err)
		}
	}
	for _, f := range found {
		t.Errorf("direct mux registration outside router/health: %s", f)
	}
}

// isMuxReceiver reports whether x names a ServeMux: the variable "mux", a
// field "mux", or the package-level http default mux.
func isMuxReceiver(x ast.Expr) bool {
	switch v := x.(type) {
	case *ast.Ident:
		return v.Name == "mux" || v.Name == "http"
	case *ast.SelectorExpr:
		return v.Sel.Name == "mux"
	}
	return false
}

// specPolicy restates the access policy of each route from spec section 8
// independently of the registry, so a wrong policy in a feature's Register
// fails here instead of being echoed by the matrix.
var specPolicy = map[string]struct {
	access      string // public | driver | admin | any
	legalExempt bool
}{
	"GET /v1/legal/current":            {access: "public"},
	"POST /v1/auth/register":           {access: "public"},
	"POST /v1/auth/verify":             {access: "public"},
	"POST /v1/auth/verify/resend":      {access: "public"},
	"POST /v1/auth/password/forgot":    {access: "public"},
	"POST /v1/auth/password/reset":     {access: "public"},
	"POST /v1/auth/invitations/accept": {access: "public"},
	"POST /v1/auth/login":              {access: "public"},
	"POST /v1/auth/refresh":            {access: "public"},
	"POST /v1/auth/logout":             {access: "any", legalExempt: true},
	"POST /v1/auth/logout-all":         {access: "any", legalExempt: true},
	"GET /v1/me":                       {access: "any", legalExempt: true},
	"PATCH /v1/me":                     {access: "any"},
	"DELETE /v1/me":                    {access: "driver", legalExempt: true},
	"POST /v1/me/legal-acceptance":     {access: "driver", legalExempt: true},
	"GET /v1/vehicles":                 {access: "driver"},
	"POST /v1/vehicles":                {access: "driver"},
	"GET /v1/vehicles/{id}":            {access: "driver"},
	"PATCH /v1/vehicles/{id}":          {access: "driver"},
	"DELETE /v1/vehicles/{id}":         {access: "driver"},
	"GET /v1/admin/admins":             {access: "admin"},
	"POST /v1/admin/admins":            {access: "admin"},
}

func TestRegistryMatchesSpecPolicies(t *testing.T) {
	e := newAPIEnv(t)
	for _, rt := range e.routes {
		want, ok := specPolicy[rt.Pattern]
		if !ok {
			t.Errorf("route %s has no spec policy", rt.Pattern)
			continue
		}
		got := "public"
		if !rt.Policy.Public {
			switch {
			case slices.Equal(rt.Policy.Roles, router.DriverOnly):
				got = "driver"
			case slices.Equal(rt.Policy.Roles, router.AdminOnly):
				got = "admin"
			case slices.Equal(rt.Policy.Roles, router.AnyRole):
				got = "any"
			default:
				got = "custom"
			}
		}
		if got != want.access {
			t.Errorf("%s: access %s, spec says %s", rt.Pattern, got, want.access)
		}
		if rt.Policy.LegalExempt != want.legalExempt {
			t.Errorf("%s: legalExempt %v, spec says %v", rt.Pattern, rt.Policy.LegalExempt, want.legalExempt)
		}
	}
	if len(specPolicy) != len(e.routes) {
		t.Errorf("spec lists %d routes, registry has %d", len(specPolicy), len(e.routes))
	}
}
