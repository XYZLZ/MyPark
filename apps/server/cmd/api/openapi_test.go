package main

import (
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

// The M2 public lot and device operations are published before they are served. This must
// not pass vacuously: the seven operationIds have to exist, each is either still
// pending or already served, and a pending one is never in the router. Serving
// an operation without removing its marker fails too.
func TestOpenAPI_PendingOperationsAreNotServed(t *testing.T) {
	ops := loadOperations(t)
	served := servedRoutes()

	byID := map[string]string{}
	for key, op := range ops {
		if op.id != "" {
			byID[op.id] = key
		}
	}
	for _, id := range []string{
		"listLots", "getLot", "getLotAvailability", "getLotSpaces", "getLotTariff", "streamLotAvailability",
		"postDeviceEvent",
	} {
		key, ok := byID[id]
		if !ok {
			t.Errorf("operationId %s is not in openapi.yaml", id)
			continue
		}
		if ops[key].pending == "" && !served[key] {
			t.Errorf("%s (%s) is neither marked x-mypark-pending nor served", id, key)
		}
	}

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
