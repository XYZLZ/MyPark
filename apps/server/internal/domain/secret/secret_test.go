package secret_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
)

const plain = "hunter2-secret"

func TestSecret_NeverPrints(t *testing.T) {
	s := secret.String(plain)
	type wrapper struct {
		Password secret.String `json:"password"`
	}
	w := wrapper{Password: s}

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("direct", slog.Any("s", s))
	log.Info("nested", slog.Any("w", w))

	jb, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{
		"%v":    fmt.Sprintf("%v", s),
		"%+v":   fmt.Sprintf("%+v", s),
		"%#v":   fmt.Sprintf("%#v", s),
		"%s":    fmt.Sprintf("%s", s), //nolint:staticcheck // S1025: the %s verb is what this test exercises
		"%v w":  fmt.Sprintf("%v", w),
		"%+v w": fmt.Sprintf("%+v", w),
		"json":  string(jb),
		"slog":  buf.String(),
	}
	for name, out := range outputs {
		if strings.Contains(out, plain) {
			t.Errorf("%s leaked plaintext: %q", name, out)
		}
		if !strings.Contains(out, "[redacted]") {
			t.Errorf("%s missing [redacted]: %q", name, out)
		}
	}
	if s.Reveal() != plain {
		t.Errorf("Reveal = %q", s.Reveal())
	}
}

func TestSecret_UnmarshalAcceptsJSONString(t *testing.T) {
	var v struct {
		P secret.String `json:"p"`
	}
	if err := json.Unmarshal([]byte(`{"p":"`+plain+`"}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.P.Reveal() != plain {
		t.Errorf("Reveal = %q", v.P.Reveal())
	}
	if err := json.Unmarshal([]byte(`{"p":5}`), &v); err == nil {
		t.Error("non-string accepted")
	}
}

func TestBytes_NeverPrints(t *testing.T) {
	const plainBytes = "device-secret-plaintext"
	b := secret.Bytes(plainBytes)
	type wrapper struct {
		Key secret.Bytes `json:"key"`
	}
	w := wrapper{Key: b}

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("direct", slog.Any("b", b))
	log.Info("nested", slog.Any("w", w))

	jb, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{
		"%v":    fmt.Sprintf("%v", b),
		"%+v":   fmt.Sprintf("%+v", b),
		"%#v":   fmt.Sprintf("%#v", b),
		"%s":    fmt.Sprintf("%s", b), //nolint:staticcheck // S1025: the %s verb is what this test exercises
		"%v w":  fmt.Sprintf("%v", w),
		"%+v w": fmt.Sprintf("%+v", w),
		"json":  string(jb),
		"slog":  buf.String(),
	}
	for name, got := range outputs {
		if strings.Contains(got, plainBytes) {
			t.Errorf("%s leaks the plaintext: %s", name, got)
		}
		if !strings.Contains(got, "[redacted]") {
			t.Errorf("%s lacks the redaction marker: %s", name, got)
		}
	}
	if string(b.Reveal()) != plainBytes {
		t.Error("Reveal must return the plaintext")
	}
}

func TestSecret_NeverPrintsUnderAnyVerb(t *testing.T) {
	verbs := []string{"%v", "%+v", "%#v", "%s", "%d", "%x", "%X", "%q", "%c", "%o", "%b", "%U", "%e", "%t", "%10s", "%-10v"}
	values := map[string]any{
		"String": secret.String("hunter2-secret"),
		"Bytes":  secret.Bytes("device-secret-plaintext"),
	}
	for name, v := range values {
		for _, verb := range verbs {
			got := fmt.Sprintf(verb, v)
			if !strings.Contains(got, "[redacted]") {
				t.Errorf("%s %s = %q; want the redaction marker", name, verb, got)
			}
			for _, leak := range []string{"hunter2", "device-secret", "104 117", "6875", "6465"} {
				if strings.Contains(got, leak) {
					t.Errorf("%s %s leaks %q: %s", name, verb, leak, got)
				}
			}
		}
	}
}
