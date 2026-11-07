// Package vehicles implements the driver-owned vehicle registry (FR-VEH-1…5):
// plate normalization, the per-driver cap and owner-scoped CRUD.
package vehicles

import (
	"strings"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
)

const (
	minPlateLen     = 2
	maxPlateLen     = 10
	maxDisplayBytes = 15
)

func invalidPlate() error {
	return apperr.Invalid(errcodes.ValidationFailed).WithDetails(map[string]any{
		"fields": []validation.FieldError{{Field: "plate", Rule: "plate"}},
	})
}

// NormalizePlate returns the canonical form used for uniqueness (upper case,
// spaces and dashes removed) and the display form (the trimmed input). Only
// ASCII letters and digits are accepted: case folding is done on ASCII only,
// so look-alikes such as the dotless i never map onto a Latin letter.
func NormalizePlate(raw string) (normalized, display string, err error) {
	display = strings.TrimSpace(raw)
	if len(display) > maxDisplayBytes {
		return "", "", invalidPlate()
	}
	var b strings.Builder
	for i := 0; i < len(display); i++ {
		switch c := display[i]; {
		case c == ' ' || c == '-':
		case c >= 'a' && c <= 'z':
			b.WriteByte(c - 'a' + 'A')
		case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteByte(c)
		default:
			return "", "", invalidPlate()
		}
	}
	normalized = b.String()
	if len(normalized) < minPlateLen || len(normalized) > maxPlateLen {
		return "", "", invalidPlate()
	}
	return normalized, display, nil
}
