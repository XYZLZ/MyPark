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
