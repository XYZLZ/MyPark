package tariffs

import (
	"context"
	"time"
)

// Repo reads tariff versions. Both methods return ErrNoVersion when nothing
// matches.
type Repo interface {
	// At returns the latest version of the lot effective at t.
	At(ctx context.Context, lotID int64, t time.Time) (Version, error)
	ByID(ctx context.Context, id int64) (Version, error)
}
