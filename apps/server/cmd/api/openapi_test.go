package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The OpenAPI document and the route registry describe the same set of
// operations: a route without a documented operation, or the reverse, fails.
func TestOpenAPI_CoversRegisteredRoutes(t *testing.T) {
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
	documented := map[string]bool{}
	for path, ops := range doc.Paths {
		for method := range ops {
			switch method {
			case "get", "put", "post", "delete", "options", "head", "patch", "trace":
				documented[strings.ToUpper(method)+" "+path] = true
			}
		}
	}

	_, routes := newHandlerWithRoutes(testDeps(readyHealth(), nil))
	want := map[string]bool{"GET /healthz": true, "GET /readyz": true}
	for _, rt := range routes {
		want[rt.Pattern] = true
	}
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
