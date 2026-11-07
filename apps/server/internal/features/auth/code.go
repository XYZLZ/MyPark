package auth

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// Code lifetimes and limits (spec 7.5).
const (
	CodeTTL             = 15 * time.Minute
	InviteTTL           = 48 * time.Hour
	MaxCodeAttempts     = 5
	codeDigits          = 6
	codeSpace           = 1_000_000
	codeRejectAtOrAbove = (1 << 32) / codeSpace * codeSpace
)

// Purpose is what a one-time code is for. The values match the
// auth_codes.purpose check.
type Purpose string

// Code purposes.
const (
	PurposeVerifyEmail   Purpose = "verify_email"
	PurposeResetPassword Purpose = "reset_password"
	PurposeAdminInvite   Purpose = "admin_invite"
)

// NewCode returns a uniformly distributed 6-digit code read from r. Values
// that would bias the modulo are rejected and redrawn.
func NewCode(r io.Reader) (string, error) {
	var buf [4]byte
	for {
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return "", fmt.Errorf("auth: read code entropy: %w", err)
		}
		if v := binary.BigEndian.Uint32(buf[:]); v < codeRejectAtOrAbove {
			return fmt.Sprintf("%0*d", codeDigits, v%codeSpace), nil
		}
	}
}
