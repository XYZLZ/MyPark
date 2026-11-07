package auth

import (
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
)

func TestPasswordPolicy(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantRule string // "" means valid
	}{
		{"9 chars is too short", "abcdefgh1", "min"},
		{"129 chars is too long", strings.Repeat("a", 129), "max"},
		{"common password, lower case", "charlie123", "password_common"},
		{"common password, any case", "ChArLiE123", "password_common"},
		{"10 runes of unicode passphrase", "ñandú-ciel", ""},
		{"9 runes of multi-byte text is too short", "ñññññññññ", "min"},
		{"strong password", "Zx9!kelp-orbit", ""},
	}
	v := validation.New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Struct(registerRequest{
				Email: "ana@example.com", Password: secret.String(tt.password),
				FirstName: "Ana", LastName: "Pérez",
				AcceptedTermsVersion: "v", AcceptedPrivacyVersion: "v",
			})
			if tt.wantRule == "" {
				if err != nil {
					t.Fatalf("Struct = %v, want nil", err)
				}
				return
			}
			verr, ok := err.(*validation.Error)
			if !ok || len(verr.Fields) != 1 || verr.Fields[0].Field != "password" || verr.Fields[0].Rule != tt.wantRule {
				t.Fatalf("Struct = %v, want password/%s", err, tt.wantRule)
			}
		})
	}
}
