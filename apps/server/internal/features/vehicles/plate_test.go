package vehicles_test

import (
	"errors"
	"regexp"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/vehicles"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
)

func TestNormalizePlate(t *testing.T) {
	cases := []struct {
		name, in, norm, display string
		wantErr                 bool
	}{
		{"dashes and spaces", "a-123 456", "A123456", "a-123 456", false},
		{"trimmed", " ab12 ", "AB12", "ab12", false},
		{"display kept", "ab-12", "AB12", "ab-12", false},
		{"two chars", "ab", "AB", "ab", false},
		{"ten chars", "ABCDEFGHIJ", "ABCDEFGHIJ", "ABCDEFGHIJ", false},
		{"accent", "Á123", "", "", true},
		{"one char", "A", "", "", true},
		{"eleven chars", "ABCDEFGHIJK", "", "", true},
		{"symbol", "A#12", "", "", true},
		{"non-ascii space", "a 123", "", "", true},
		{"dotless i", "ıı12", "", "", true},
		{"tab inside", "A\t12", "", "", true},
		{"blank", "   ", "", "", true},
		{"empty", "", "", "", true},
		{"display too long", "A-------------------B", "", "", true},
		{"only separators", "- -", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			norm, display, err := vehicles.NormalizePlate(tc.in)
			if tc.wantErr {
				ae, ok := errors.AsType[*apperr.Error](err)
				if !ok || ae.Kind != apperr.KindInvalid {
					t.Fatalf("err = %v, want apperr Invalid", err)
				}
				fields, _ := ae.Details["fields"].([]validation.FieldError)
				if len(fields) != 1 || fields[0].Field != "plate" || fields[0].Rule != "plate" {
					t.Fatalf("details = %#v, want one field plate/plate", ae.Details)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if norm != tc.norm || display != tc.display {
				t.Errorf("got (%q, %q), want (%q, %q)", norm, display, tc.norm, tc.display)
			}
		})
	}
}

func FuzzNormalizePlate(f *testing.F) {
	for _, s := range []string{"a-123 456", "", "Á", "A\x00", "ıſ", " ab12 ", "A#12"} {
		f.Add(s)
	}
	re := regexp.MustCompile(`^[A-Z0-9]{2,10}$`)
	f.Fuzz(func(t *testing.T, in string) {
		norm, display, err := vehicles.NormalizePlate(in)
		if err != nil {
			return
		}
		if !re.MatchString(norm) {
			t.Fatalf("normalized %q does not match the plate pattern", norm)
		}
		if n := len(display); n < 2 || n > 15 {
			t.Fatalf("display %q has length %d", display, n)
		}
	})
}
