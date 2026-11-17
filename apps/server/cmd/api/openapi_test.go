package main

import (
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// operation is the part of an OpenAPI operation these tests look at.
type operation struct {
	id      string
	pending string
	service string // path-level x-mypark-service; empty means this API
}

// loadOperations returns every documented operation keyed "METHOD /path".
func loadOperations(t *testing.T) map[string]operation {
	t.Helper()
	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	ops := map[string]operation{}
	for path, item := range doc.Paths {
		service, _ := item["x-mypark-service"].(string)
		for method, v := range item {
			switch method {
			case "get", "put", "post", "delete", "options", "head", "patch", "trace":
			default:
				continue
			}
			op := operation{service: service}
			if m, ok := v.(map[string]any); ok {
				op.id, _ = m["operationId"].(string)
				op.pending, _ = m["x-mypark-pending"].(string)
			}
			ops[strings.ToUpper(method)+" "+path] = op
		}
	}
	return ops
}

// servedRoutes returns the router's patterns plus the two probe routes.
func servedRoutes() map[string]bool {
	_, routes := newHandlerWithRoutes(testDeps(readyHealth(), nil))
	served := map[string]bool{"GET /healthz": true, "GET /readyz": true}
	for _, rt := range routes {
		served[rt.Pattern] = true
	}
	return served
}

// The OpenAPI document and the route registry describe the same set of
// operations: a route without a documented operation, or the reverse, fails.
//
// A path carrying x-mypark-service: simulator belongs to the simulator control
// API (M2C-49), not to this router, and is skipped here.
//
// An operation carrying x-mypark-pending (a slice id) is published ahead of its
// implementation (contract first, M2C-12) and is skipped here until that slice
// serves it and removes the marker.
func TestOpenAPI_CoversRegisteredRoutes(t *testing.T) {
	documented := map[string]bool{}
	for key, op := range loadOperations(t) {
		if op.pending != "" || op.service == "simulator" {
			continue
		}
		documented[key] = true
	}

	want := servedRoutes()
	for p := range want {
		if !documented[p] {
			t.Errorf("%s is served but not documented in openapi.yaml", p)
		}
	}
	var extra []string
	for p := range documented {
		if !want[p] {
			extra = append(extra, p)
		}
	}
	slices.Sort(extra)
	for _, p := range extra {
		t.Errorf("%s is documented in openapi.yaml but not served", p)
	}
}

var pendingSliceID = regexp.MustCompile(`^(S|C)[0-9.]+$`)

// An operation published before it is served (contract first, M2C-12)
// carries x-mypark-pending with a slice id and is never in the router;
// serving it without removing the marker fails. The M2 lot and device
// operations are all served now, so TestOpenAPI_CoversRegisteredRoutes
// checks them like any other route; no list of them is kept here.
func TestOpenAPI_PendingOperationsAreNotServed(t *testing.T) {
	ops := loadOperations(t)
	served := servedRoutes()
	for key, op := range ops {
		if op.pending == "" {
			continue
		}
		if !pendingSliceID.MatchString(op.pending) {
			t.Errorf("%s: x-mypark-pending %q does not match %s", key, op.pending, pendingSliceID)
		}
		if served[key] {
			t.Errorf("%s is served but still marked x-mypark-pending", key)
		}
	}
}

// The simulator control paths live in the same document but are served by
// another process. They must exist and be tagged, so the API parity test can
// skip them without hiding a real undocumented route.
func TestOpenAPI_SimulatorPathsSkippedByAPI(t *testing.T) {
	var control []string
	for key, op := range loadOperations(t) {
		_, path, _ := strings.Cut(key, " ")
		if !strings.HasPrefix(path, "/control/") {
			if op.service == "simulator" {
				t.Errorf("%s is tagged simulator but is not under /control/", key)
			}
			continue
		}
		control = append(control, key)
		if op.service != "simulator" {
			t.Errorf("%s must carry x-mypark-service: simulator", key)
		}
	}
	if len(control) < 7 {
		t.Errorf("want at least 7 operations under /control/, got %d: %v", len(control), control)
	}

	served := servedRoutes()
	for _, key := range control {
		if served[key] {
			t.Errorf("%s is a simulator route but the API serves it", key)
		}
	}
}

// postDeviceEvent is signed by the device HMAC and nothing else (design §7.3,
// §7.8): exactly one security requirement holding the three device schemes
// together, each with no scopes. openapi.d.ts drops security, so only this
// test pins it.
func TestOpenAPI_PostDeviceEventSecurity(t *testing.T) {
	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	var doc struct {
		Paths map[string]struct {
			Post *struct {
				OperationID string                `yaml:"operationId"`
				Security    []map[string][]string `yaml:"security"`
			} `yaml:"post"`
		} `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	op := doc.Paths["/v1/devices/events"].Post
	if op == nil || op.OperationID != "postDeviceEvent" {
		t.Fatalf("POST /v1/devices/events is not postDeviceEvent: %+v", op)
	}
	if len(op.Security) != 1 {
		t.Fatalf("security has %d requirement objects, want exactly 1: %v", len(op.Security), op.Security)
	}
	req := op.Security[0]
	keys := slices.Sorted(maps.Keys(req))
	if want := []string{"deviceId", "deviceSignature", "deviceTimestamp"}; !slices.Equal(keys, want) {
		t.Errorf("security schemes = %v, want %v", keys, want)
	}
	for k, scopes := range req {
		if len(scopes) != 0 {
			t.Errorf("%s has scopes %v, want none", k, scopes)
		}
	}
}

// m3PendingAllowlist holds the M3 slices that still owe a served operation.
// TestOpenAPI_NoPendingOperations skips them, so the M3 contract can ship in S1
// ahead of its handlers; each serving task removes its marker from openapi.yaml
// and S9.1 deletes this list, which makes the test strict again.
var m3PendingAllowlist = map[string]bool{"S4.2": true, "S5.1": true, "S5.3": true}

// m3Operations is the M3 driver contract published by S1, with the slice that
// serves each operation and removes its marker (design §14.1).
var m3Operations = []struct{ key, id, marker string }{
	{"POST /v1/reservations/quote", "quoteReservation", "S3.2"},
	{"POST /v1/reservations", "createReservation", "S3.2"},
	{"GET /v1/reservations", "listReservations", "S3.3"},
	{"GET /v1/reservations/{id}", "getReservation", "S3.3"},
	{"PATCH /v1/reservations/{id}", "changeReservationVehicle", "S3.3"},
	{"POST /v1/reservations/{id}/pay", "payReservation", "S4.2"},
	{"GET /v1/reservations/{id}/qr", "getReservationQr", "S4.2"},
	{"POST /v1/reservations/{id}/cancel", "cancelReservation", "S5.1"},
	{"GET /v1/payments", "listPayments", "S5.3"},
	{"GET /v1/payments/{id}", "getPayment", "S5.3"},
}

// Each M3 operation is documented under its operationId and carries either its
// own slice marker (not yet served) or none (the serving task removed it).
// Every marker that is present is in the allowlist, so a typo or a marker
// moved to another slice fails here instead of hiding in the skipped set.
func TestOpenAPI_M3PendingMarkersMatchAllowlist(t *testing.T) {
	ops := loadOperations(t)
	for _, want := range m3Operations {
		op, ok := ops[want.key]
		if !ok {
			t.Errorf("%s is not documented in openapi.yaml", want.key)
			continue
		}
		if op.id != want.id {
			t.Errorf("%s: operationId = %q, want %q", want.key, op.id, want.id)
		}
		if op.pending != "" && op.pending != want.marker {
			t.Errorf("%s: x-mypark-pending = %q, want %q or none", want.key, op.pending, want.marker)
		}
	}
	for key, op := range ops {
		if op.pending != "" && strings.HasPrefix(op.pending, "S") && !m3PendingAllowlist[op.pending] {
			t.Errorf("%s: marker %q is not in m3PendingAllowlist", key, op.pending)
		}
	}
}

// Every operation of the contract is served: contract-first markers
// (x-mypark-pending) are removed by the slice that serves the operation, and
// none may remain at the end of M2. A failure names the slice left behind.
func TestOpenAPI_NoPendingOperations(t *testing.T) {
	for key, op := range loadOperations(t) {
		if m3PendingAllowlist[op.pending] {
			continue
		}
		if op.pending != "" {
			t.Errorf("%s still carries x-mypark-pending %q: slice %s did not remove it", key, op.pending, op.pending)
		}
	}
}
