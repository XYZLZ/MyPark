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

// Every operation of the contract is served: contract-first markers
// (x-mypark-pending) are removed by the slice that serves the operation, and
// none may remain once the milestone is built. A failure names the slice left behind.
func TestOpenAPI_NoPendingOperations(t *testing.T) {
	for key, op := range loadOperations(t) {
		if op.pending != "" {
			t.Errorf("%s still carries x-mypark-pending %q: slice %s did not remove it", key, op.pending, op.pending)
		}
	}
}

// Every documented operation, served, pending or simulator, has a non-empty
// operationId, and no two share one: the generated client names its calls
// after them.
func TestOpenAPI_OperationIDsPresentAndUnique(t *testing.T) {
	seen := map[string]string{}
	for key, op := range loadOperations(t) {
		if op.id == "" {
			t.Errorf("%s has no operationId", key)
			continue
		}
		if other, dup := seen[op.id]; dup {
			t.Errorf("operationId %q is used by both %s and %s", op.id, other, key)
		}
		seen[op.id] = key
	}
}
