package lots

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrNotFound means no lot has the given public id.
var ErrNotFound = errors.New("lots: lot not found")

// Resolver maps a lot's public id to its internal id.
type Resolver interface {
	LotIDByPublicID(ctx context.Context, id uuid.UUID) (int64, error)
}
