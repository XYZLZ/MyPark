package legal

import (
	"context"
	"time"
)

// Repo reads the legal documents.
type Repo interface {
	// Current returns the latest document of each kind published at or before
	// now. It fails when either kind has no published document.
	Current(ctx context.Context, now time.Time) (Versions, error)
}
