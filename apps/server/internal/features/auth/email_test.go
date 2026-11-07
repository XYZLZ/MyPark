package auth

import "testing"

func TestNormalizeEmail(t *testing.T) {
	tests := map[string]string{
		" Ana@Example.COM ":   "ana@example.com",
		"ana@example.com":     "ana@example.com",
		"\tANA@EXAMPLE.com\n": "ana@example.com",
		"":                    "",
	}
	for in, want := range tests {
		if got := NormalizeEmail(in); got != want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", in, got, want)
		}
	}
}
