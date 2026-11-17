package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/router"
)

// matrixEntry says how to call one route. build returns a request whose body
// and path are valid for the route; the runner adds the bearer token. owned
// marks routes addressed at a resource of the owner (vehicles/{id}).
type matrixEntry struct {
	build func(f *fixture) *http.Request
	owned bool
	// reservation seeds a confirmed reservation of the owner (f.reservationID)
	// before the run; notFound is the code of the owned 404 (default: a vehicle).
	reservation bool
	notFound    errcodes.Code
}

func vehiclePath(f *fixture) string { return "/v1/vehicles/" + f.vehicleID.String() }

func reservationPath(f *fixture) string { return "/v1/reservations/" + f.reservationID.String() }

// seedReservation gives the owner a confirmed reservation of its vehicle, with
// the arrival an hour ahead, straight into the database.
func (f *fixture) seedReservation() {
	f.env.t.Helper()
	lotID := testsupport.SeedPilotLot(f.env.t, f.env.db)
	now := f.env.clk.Now()
	err := f.env.db.Super.QueryRow(f.env.t.Context(),
		`insert into reservations (user_id, vehicle_id, lot_id, arrival_at, hold_window, status, fee_cents,
		                           tariff_version_id, policy_version_id, payment_due_at, created_at, updated_at,
		                           confirmed_at, qr_nonce)
		 select $1, v.id, $2, $3::timestamptz, tstzrange($3::timestamptz - interval '15 minutes', $3::timestamptz + interval '20 minutes', '[)'),
		        'confirmed', 15000,
		        (select id from tariff_versions where lot_id = $2 order by effective_from desc limit 1),
		        (select id from policy_versions where lot_id = $2 order by effective_from desc limit 1),
		        $4::timestamptz, $4::timestamptz, $4::timestamptz, $4::timestamptz, decode(md5(random()::text), 'hex')
		 from vehicles v where v.public_id = $5
		 returning public_id`,
		f.owner.userID, lotID, now.Add(time.Hour), now, f.vehicleID).Scan(&f.reservationID)
	if err != nil {
		f.env.t.Fatalf("seed fixture reservation: %v", err)
	}
}

// pilotLotPublicID seeds the pilot lot (idempotent) and returns its public id.
func pilotLotPublicID(f *fixture) string {
	f.env.t.Helper()
	lotID := testsupport.SeedPilotLot(f.env.t, f.env.db)
	var pub uuid.UUID
	if err := f.env.db.App.QueryRow(f.env.t.Context(), "select public_id from lots where id = $1", lotID).Scan(&pub); err != nil {
		f.env.t.Fatal(err)
	}
	return pub.String()
}

const streamPattern = "GET /v1/lots/{id}/availability/stream"

// streamReq builds a stream request that ends after 200 ms, so a stream
// served into a ResponseRecorder returns instead of running forever.
func streamReq(path string) *http.Request {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	time.AfterFunc(time.Second, cancel)
	return httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
}

// matrix has one entry per route pattern of the registry. TestAuthzMatrix
// fails for a registered route that is missing here.
var matrix = map[string]matrixEntry{
	"GET /v1/legal/current": {build: func(*fixture) *http.Request { return jsonReq("GET", "/v1/legal/current", nil) }},

	"GET /v1/lots": {build: func(*fixture) *http.Request { return jsonReq("GET", "/v1/lots", nil) }},
	"GET /v1/lots/{id}": {build: func(*fixture) *http.Request {
		return jsonReq("GET", "/v1/lots/"+uuid.NewString(), nil)
	}},
	"GET /v1/lots/{id}/availability": {build: func(*fixture) *http.Request {
		return jsonReq("GET", "/v1/lots/"+uuid.NewString()+"/availability", nil)
	}},
	"GET /v1/lots/{id}/spaces": {build: func(*fixture) *http.Request {
		return jsonReq("GET", "/v1/lots/"+uuid.NewString()+"/spaces", nil)
	}},
	"GET /v1/lots/{id}/tariff": {build: func(*fixture) *http.Request {
		return jsonReq("GET", "/v1/lots/"+uuid.NewString()+"/tariff", nil)
	}},
	streamPattern: {build: func(*fixture) *http.Request {
		return streamReq("/v1/lots/" + uuid.NewString() + "/availability/stream")
	}},

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

	"POST /v1/reservations/quote": {build: func(f *fixture) *http.Request {
		return jsonReq("POST", "/v1/reservations/quote", map[string]any{
			"lot_id": pilotLotPublicID(f), "arrival_at": "2026-11-21T15:00:00Z",
		})
	}},
	// The vehicle belongs to the owner: another driver's id is a 404 like a missing one.
	"POST /v1/reservations": {owned: true, build: func(f *fixture) *http.Request {
		return jsonReq("POST", "/v1/reservations", map[string]any{
			"lot_id": pilotLotPublicID(f), "vehicle_id": f.vehicleID.String(), "arrival_at": "2026-11-21T15:00:00Z",
		})
	}},

	"GET /v1/reservations/{id}": {owned: true, reservation: true, notFound: errcodes.ReservationNotFound,
		build: func(f *fixture) *http.Request { return jsonReq("GET", reservationPath(f), nil) }},

	"GET /v1/admin/admins": {build: func(*fixture) *http.Request { return jsonReq("GET", "/v1/admin/admins", nil) }},

	// Unsigned: the user rows carry at most a bearer, which a device route
	// ignores. The device principals are signed in deviceRows.
	"POST /v1/devices/events": {build: func(*fixture) *http.Request {
		return jsonReq("POST", "/v1/devices/events", map[string]any{
			"event_id": uuid.NewString(), "type": "heartbeat", "occurred_at": "2026-11-20T11:59:59Z", "payload": map[string]any{},
		})
	}},
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
	// A device route ignores bearer tokens: every user principal is anonymous
	// to it (design §7.8).
	if p.Device {
		return outcome{http.StatusUnauthorized, string(errcodes.DeviceUnauthenticated)}
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
			if entry.reservation {
				f.seedReservation()
			}
			for _, r := range rows {
				want := expected(rt.Policy, r)
				if entry.owned && r.name == "other-driver" {
					// Not yours and not there look the same (spec section 8).
					want = outcome{http.StatusNotFound, string(entry.notFoundCode())}
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
				// Passed the gates. A protected route must succeed for a valid
				// request; a public one only has to reach its handler.
				if rt.Policy.Public {
					if slices.Contains(gateCodes, errCode(body)) || rec.Code >= http.StatusInternalServerError ||
						rec.Code == http.StatusNotFound && errCode(body) == string(errcodes.HTTPNotFound) {
						t.Errorf("%s: got %d %q, want the request to reach the handler (body %s)",
							r.name, rec.Code, errCode(body), rec.Body)
					}
				} else if rec.Code < 200 || rec.Code >= 300 {
					t.Errorf("%s: got %d %q, want 2xx (body %s)", r.name, rec.Code, errCode(body), rec.Body)
				}
			}
			if rt.Policy.Device {
				deviceRows(t, e)
			}
		})
	}
}

// deviceRows are the device principals of a device route (design §7.8): a
// signed device sending a type its kind allows passes, any other type is 403.
func deviceRows(t *testing.T, e *apiEnv) {
	t.Helper()
	de := &deviceEnv{apiEnv: e, lotID: testsupport.SeedPilotLot(t, e.db)}
	podium := de.device(authz.DevicePodium)
	gateway := de.device(authz.DeviceSensorGateway)

	if rec := de.send(podium, de.eventBody(uuid.New(), "heartbeat", map[string]any{})); rec.Code != http.StatusOK {
		t.Errorf("device, allowed type: got %d %s, want 200", rec.Code, rec.Body)
	}
	scan := map[string]any{"credential": map[string]any{"kind": "ticket", "value": "ABCD1234EFGH5678"}}
	rec := de.send(gateway, de.eventBody(uuid.New(), "paystation.scan", scan))
	if rec.Code != http.StatusForbidden || errCode(decodeBody(t, rec)) != string(errcodes.DeviceEventNotAllowed) {
		t.Errorf("device, disallowed type: got %d %s, want 403 %s", rec.Code, rec.Body, errcodes.DeviceEventNotAllowed)
	}
}

// assertSameAsMissing checks that another driver's 404 equals the 404 for an
// id that does not exist, apart from meta.request_id.
func assertSameAsMissing(t *testing.T, e *apiEnv, f *fixture, entry matrixEntry, got *httptest.ResponseRecorder) {
	t.Helper()
	missing := *f
	missing.vehicleID = uuid.New()
	missing.reservationID = uuid.New()
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

	// Any 2-argument Handle/HandleFunc call with a literal pattern registers
	// straight on a mux, whatever the receiver is called. router.Router.Handle
	// takes 3 arguments and router.go passes a variable, so neither matches.
	// The health feature may register only the allow-listed probes.
	root := filepath.Join("..", "..")
	healthFile := filepath.Join("internal", "features", "health", "handler.go")
	var found []string
	for _, dir := range []string{filepath.Join("cmd", "api"), "internal"} {
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
				if !ok || len(call.Args) != 2 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
					return true
				}
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				pattern, err := strconv.Unquote(lit.Value)
				if err != nil {
					pattern = lit.Value
				}
				if rel == healthFile && allowList[pattern] {
					return true
				}
				found = append(found, rel+": "+sel.Sel.Name+"("+lit.Value+")")
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", dir, err)
		}
	}
	for _, f := range found {
		t.Errorf("direct mux registration outside the router and the health allow-list: %s", f)
	}
}

// specPolicy restates the access policy of each route from spec section 8
// independently of the registry, so a wrong policy in a feature's Register
// fails here instead of being echoed by the matrix.
var specPolicy = map[string]struct {
	access      string // public | driver | admin | any | device
	legalExempt bool
	limit       *ratelimit.Rule // the route's Policy.Limit; nil means none
	// deviceLimits are the rules a device route consults, in order.
	deviceLimits []ratelimit.Rule
}{
	"GET /v1/legal/current":            {access: "public"},
	"GET /v1/lots":                     {access: "public", limit: &ratelimit.PublicReadIP},
	"GET /v1/lots/{id}":                {access: "public", limit: &ratelimit.PublicReadIP},
	"GET /v1/lots/{id}/availability":   {access: "public", limit: &ratelimit.PublicReadIP},
	"GET /v1/lots/{id}/spaces":         {access: "public", limit: &ratelimit.PublicReadIP},
	"GET /v1/lots/{id}/tariff":         {access: "public", limit: &ratelimit.PublicReadIP},
	streamPattern:                      {access: "public", limit: &ratelimit.SSEConnectIP},
	"POST /v1/auth/register":           {access: "public", limit: &ratelimit.RegisterIP},
	"POST /v1/auth/verify":             {access: "public", limit: &ratelimit.CodeSubmitIP},
	"POST /v1/auth/verify/resend":      {access: "public", limit: &ratelimit.CodeRequestIP},
	"POST /v1/auth/password/forgot":    {access: "public", limit: &ratelimit.CodeRequestIP},
	"POST /v1/auth/password/reset":     {access: "public", limit: &ratelimit.CodeSubmitIP},
	"POST /v1/auth/invitations/accept": {access: "public", limit: &ratelimit.CodeSubmitIP},
	"POST /v1/auth/login":              {access: "public"},
	"POST /v1/auth/refresh":            {access: "public", limit: &ratelimit.RefreshIP},
	"POST /v1/auth/logout":             {access: "any", legalExempt: true},
	"POST /v1/auth/logout-all":         {access: "any", legalExempt: true},
	"GET /v1/me":                       {access: "any", legalExempt: true},
	"PATCH /v1/me":                     {access: "any"},
	"DELETE /v1/me":                    {access: "driver", legalExempt: true, limit: &ratelimit.DeleteAccountUser},
	"POST /v1/me/legal-acceptance":     {access: "driver", legalExempt: true},
	"GET /v1/vehicles":                 {access: "driver"},
	"POST /v1/vehicles":                {access: "driver", limit: &ratelimit.VehicleWriteUser},
	"GET /v1/vehicles/{id}":            {access: "driver"},
	"PATCH /v1/vehicles/{id}":          {access: "driver", limit: &ratelimit.VehicleWriteUser},
	"DELETE /v1/vehicles/{id}":         {access: "driver", limit: &ratelimit.VehicleWriteUser},
	"POST /v1/reservations/quote":      {access: "driver"},
	"POST /v1/reservations":            {access: "driver", limit: &ratelimit.ReservationWriteUser},
	"GET /v1/reservations/{id}":        {access: "driver"},
	"GET /v1/admin/admins":             {access: "admin"},
	"POST /v1/admin/admins":            {access: "admin"},
	// A device route declares no Policy.Limit: the router applies DeviceIP
	// before DeviceAuth and the per-device rule after it.
	"POST /v1/devices/events": {access: "device", deviceLimits: []ratelimit.Rule{
		ratelimit.DeviceIP, ratelimit.DeviceRule(config.DefaultDeviceRateLimitPerS),
	}},
}

func TestRegistryMatchesSpecPolicies(t *testing.T) {
	limiter := &recordingLimiter{}
	e := newAPIEnv(t, func(d *deps) { d.limiter = limiter })
	for _, rt := range e.routes {
		want, ok := specPolicy[rt.Pattern]
		if !ok {
			t.Errorf("route %s has no spec policy", rt.Pattern)
			continue
		}
		got := "public"
		if !rt.Policy.Public {
			switch {
			case rt.Policy.Device:
				got = "device"
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
		// A device route's Policy.Limit is nil; its fixed limits are checked
		// below through the rules the live route consults.
		switch got := rt.Policy.Limit; {
		case got == nil && want.limit == nil:
		case got == nil || want.limit == nil || *got != *want.limit:
			t.Errorf("%s: limit %s, spec says %s", rt.Pattern, ruleName(got), ruleName(want.limit))
		}
		if rt.Policy.Device {
			if got := deviceRouteRules(t, e, limiter); !slices.Equal(got, want.deviceLimits) {
				t.Errorf("%s: device limits %+v, spec says %+v", rt.Pattern, got, want.deviceLimits)
			}
		} else if want.deviceLimits != nil {
			t.Errorf("%s: spec lists device limits on a non-device route", rt.Pattern)
		}
	}
	if len(specPolicy) != len(e.routes) {
		t.Errorf("spec lists %d routes, registry has %d", len(specPolicy), len(e.routes))
	}
}

// recordingLimiter allows everything and records the rules it was asked about.
type recordingLimiter struct {
	mu    sync.Mutex
	rules []ratelimit.Rule
}

func (l *recordingLimiter) Allow(_ context.Context, r ratelimit.Rule, _ string) (ratelimit.Decision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rules = append(l.rules, r)
	return ratelimit.Decision{Allowed: true}, nil
}

// deviceRouteRules sends one signed heartbeat and returns the rules the route
// consulted, in order.
func deviceRouteRules(t *testing.T, e *apiEnv, l *recordingLimiter) []ratelimit.Rule {
	t.Helper()
	de := &deviceEnv{apiEnv: e, lotID: testsupport.SeedPilotLot(t, e.db)}
	d := de.device(authz.DevicePodium)
	l.mu.Lock()
	l.rules = nil
	l.mu.Unlock()
	if rec := de.send(d, de.eventBody(uuid.New(), "heartbeat", map[string]any{})); rec.Code != http.StatusOK {
		t.Fatalf("signed heartbeat: %d %s", rec.Code, rec.Body)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.rules)
}

func ruleName(r *ratelimit.Rule) string {
	if r == nil {
		return "none"
	}
	return r.Name
}

// notFoundCode is the code another driver's resource answers with.
func (m matrixEntry) notFoundCode() errcodes.Code {
	if m.notFound != "" {
		return m.notFound
	}
	return errcodes.VehicleNotFound
}
