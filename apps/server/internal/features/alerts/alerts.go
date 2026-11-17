// Package alerts records incidents an admin has to look at (architecture
// 11.1). M3 writes two kinds; M4 adds the views and more kinds.
package alerts

import (
	"context"
	"time"
)

// Kind is the reconciliation_alerts.kind value.
type Kind string

// The kinds M3 writes (M3B-30).
const (
	// KindReservationUnadmitted is a confirmed reservation the lot could not admit.
	KindReservationUnadmitted Kind = "reservation_unadmitted"
	// KindRefundFailed is money that was not returned automatically: a refund
	// that failed for good, or a duplicate charge that has none queued.
	KindRefundFailed Kind = "refund_failed"
)

// Recorder opens alerts.
type Recorder interface {
	// Open records an open alert in the transaction carried by ctx, so it
	// exists only if the action commits; without one it fails with
	// database.ErrNoTx. details holds public ids and short codes only, never
	// card data or provider references.
	Open(ctx context.Context, lotID int64, kind Kind, details map[string]string, now time.Time) error
}
