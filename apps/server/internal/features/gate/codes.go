package gate

import "strings"

const (
	ticketCodeLen          = 16
	cardUIDMin, cardUIDMax = 8, 20
)

// NormalizeTicketCode canonicalizes a ticket code as typed or scanned:
// uppercase, '-' and spaces dropped, the look-alikes I and L read as 1 and O
// as 0. It reports false unless the result is exactly 16 Crockford base32
// characters.
func NormalizeTicketCode(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '-' || c == ' ':
			continue
		case c >= 'a' && c <= 'z':
			c -= 'a' - 'A'
		}
		switch c {
		case 'I', 'L':
			c = '1'
		case 'O':
			c = '0'
		}
		if !isCrockford(c) {
			return "", false
		}
		b.WriteByte(c)
	}
	if b.Len() != ticketCodeLen {
		return "", false
	}
	return b.String(), true
}

// isCrockford reports whether c is in the Crockford base32 alphabet:
// digits and uppercase letters except I, L, O and U.
func isCrockford(c byte) bool {
	switch {
	case c >= '0' && c <= '9':
		return true
	case c >= 'A' && c <= 'Z':
		return c != 'I' && c != 'L' && c != 'O' && c != 'U'
	}
	return false
}

// NormalizeCardUID canonicalizes an employee card UID: uppercase hex with
// ':' '-' and spaces dropped. It reports false unless 8 to 20 hex characters
// remain.
func NormalizeCardUID(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ':' || c == '-' || c == ' ':
			continue
		case c >= 'a' && c <= 'f':
			c -= 'a' - 'A'
		}
		if (c < '0' || c > '9') && (c < 'A' || c > 'F') {
			return "", false
		}
		b.WriteByte(c)
	}
	if b.Len() < cardUIDMin || b.Len() > cardUIDMax {
		return "", false
	}
	return b.String(), true
}
