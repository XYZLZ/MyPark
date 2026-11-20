package main

import (
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/XYZLZ/MyPark/apps/server/internal/simulator"
)

// The simulator control operations in openapi.yaml (path-level
// x-mypark-service: simulator, M2C-49) are exactly the routes this binary
// serves under /control. /healthz and /readyz are probes, not contract paths
// (PF3). Each control operation is reached through the simulator's own server
// and requires the X-Simulator-Token scheme, and none is still pending.
func TestOpenAPI_CoversSimulatorRoutes(t *testing.T) {
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
	for path, item := range doc.Paths {
		if item["x-mypark-service"] != "simulator" {
			continue
		}
		servers, _ := item["servers"].([]any)
		if len(servers) != 1 || servers[0].(map[string]any)["url"] != "http://simulator:8090" {
			t.Errorf("%s: servers = %v, want exactly http://simulator:8090", path, item["servers"])
		}
		for method, v := range item {
			switch method {
			case "get", "put", "post", "delete", "patch":
			default:
				continue
			}
			key := strings.ToUpper(method) + " " + path
			documented[key] = true
			op, _ := v.(map[string]any)
			if p, ok := op["x-mypark-pending"]; ok {
				t.Errorf("%s is served but still marked x-mypark-pending %v", key, p)
			}
			sec, _ := op["security"].([]any)
			if len(sec) != 1 {
				t.Errorf("%s: security = %v, want exactly [{simulatorToken: []}]", key, op["security"])
				continue
			}
			req, _ := sec[0].(map[string]any)
			scopes, ok := req["simulatorToken"].([]any)
			if len(req) != 1 || !ok || len(scopes) != 0 {
				t.Errorf("%s: security = %v, want exactly [{simulatorToken: []}]", key, op["security"])
			}
		}
	}

	served := map[string]bool{}
	for _, p := range newControlRoutes() {
		served[p] = true
	}
	if got, want := slices.Sorted(maps.Keys(documented)), slices.Sorted(maps.Keys(served)); !slices.Equal(got, want) {
		t.Errorf("documented simulator operations\n  %v\nserved control routes\n  %v", got, want)
	}
	if len(served) != 7 {
		t.Errorf("%d control routes, want the 7 of design §13.3", len(served))
	}
}

// The InjectCarRequest schema matches the simulator's own injectRequest: the
// same credential kinds, and the qr pattern the handler enforces.
func TestSimulatorOpenAPI_InjectCarParity(t *testing.T) {
	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	var doc struct {
		Components struct {
			Schemas struct {
				Inject struct {
					Properties struct {
						Credential struct {
							Enum []string `yaml:"enum"`
						} `yaml:"credential"`
						QR struct {
							Pattern string `yaml:"pattern"`
						} `yaml:"qr"`
					} `yaml:"properties"`
				} `yaml:"InjectCarRequest"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	props := doc.Components.Schemas.Inject.Properties
	if got, want := slices.Sorted(slices.Values(props.Credential.Enum)), slices.Sorted(slices.Values(simulator.InjectCredentials())); !slices.Equal(got, want) {
		t.Errorf("credential enum = %v, injectRequest accepts %v", got, want)
	}
	if props.QR.Pattern != simulator.QRPattern() {
		t.Errorf("qr pattern = %q, handler enforces %q", props.QR.Pattern, simulator.QRPattern())
	}
}
