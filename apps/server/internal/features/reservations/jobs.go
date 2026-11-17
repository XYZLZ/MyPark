package reservations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
)

// expiryBatch is how many candidates one page of an expiry job reads (design 12).
const expiryBatch = 200

// Jobs holds the worker's reservation expiry jobs (design 12). Neither job
// ever bumps the lot version: a pending_payment reservation never counted as a
// hold, and a no-show acts at or after the end of its window, when its hold
// had already stopped counting (design 8.2, M3B-13).
type Jobs struct{ s *Service }

// NewJobs returns the expiry jobs over the Service's dependencies (Tx, Repo,
// Clock, Log, Fees and Notify).
func NewJobs(d Deps) *Jobs { return &Jobs{s: NewService(d)} }

// ExpireUnpaid expires the pending_payment reservations whose payment_due_at
// has passed at now, and returns how many it expired. Each runs in its own
// transaction in the lock order of design 8.1 (reservation, then slot loads).
// A reservation with a live fee attempt is skipped: its charge may be in
// flight, so the reconcile job settles the attempt first, and a late success
// is refunded by the Apply paths. A failed row is logged and the others
// still run; the errors are joined.
func (j *Jobs) ExpireUnpaid(ctx context.Context, now time.Time) (int, error) {
	return j.sweep(ctx, now, j.s.repo.ListDueUnpaid, j.expireUnpaidOne)
}

// ExpireNoShows expires the confirmed reservations whose hold window closed at
// or before now (design 12, FR-RES-17). The fee is forfeited. A voided
// check-in that returned to confirmed after its window closed is picked up
// here too (M3B-60). A checked_in reservation is never taken: the flows that
// lock one do so under the lot lock (design 8.1).
func (j *Jobs) ExpireNoShows(ctx context.Context, now time.Time) (int, error) {
	return j.sweep(ctx, now, j.s.repo.ListDueNoShows, j.expireNoShowOne)
}

// sweep pages through the candidates (keyset by id, so rows one() skips never
// stall the run) and counts the ones it expired.
func (j *Jobs) sweep(ctx context.Context, now time.Time,
	list func(context.Context, time.Time, int64, int) ([]int64, error),
	one func(context.Context, int64, time.Time) (bool, error),
) (int, error) {
	var done int
	var errs []error
	var after int64
	for {
		ids, err := list(ctx, now, after, expiryBatch)
		if err != nil {
			return done, errors.Join(append(errs, err)...)
		}
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return done, errors.Join(append(errs, err)...)
			}
			ok, err := one(ctx, id, now)
			if err != nil {
				j.s.log.ErrorContext(ctx, "reservations: expiry failed", "reservation_row", id, "err", err)
				errs = append(errs, err)
				continue
			}
			if ok {
				done++
			}
		}
		if len(ids) < expiryBatch {
			return done, errors.Join(errs...)
		}
		after = ids[len(ids)-1]
	}
}

func (j *Jobs) expireUnpaidOne(ctx context.Context, id int64, now time.Time) (expired bool, err error) {
	err = j.s.tx.InTx(ctx, func(ctx context.Context) error {
		expired = false
		if err := j.s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		rsv, ok, err := j.s.repo.LockDueUnpaid(ctx, id, now)
		if err != nil || !ok {
			return err
		}
		// Re-checked with the reservation lock held, as a separate statement:
		// a pay attempt starts under this same lock, so none can slip in now.
		fee, live, err := j.s.fees.LiveFee(ctx, rsv.ID)
		if err != nil {
			return err
		}
		if live {
			if fee.Status != payments.StatusPending {
				j.s.log.ErrorContext(ctx, "reservations: pending_payment reservation has a settled fee, not expiring",
					"reservation_id", rsv.PublicID, "payment_id", fee.PublicID, "payment_status", fee.Status)
			}
			return nil
		}
		if err := j.s.expireUnpaid(ctx, rsv, "", now); err != nil {
			return err
		}
		expired = true
		return nil
	})
	return expired && err == nil, err
}

func (j *Jobs) expireNoShowOne(ctx context.Context, id int64, now time.Time) (expired bool, err error) {
	err = j.s.tx.InTx(ctx, func(ctx context.Context) error {
		expired = false
		if err := j.s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		rsv, ok, err := j.s.repo.LockDueNoShow(ctx, id, now)
		if err != nil || !ok {
			return err
		}
		if err := j.s.repo.ExpireNoShow(ctx, rsv.ID, now); err != nil {
			return err
		}
		if _, err := j.s.repo.EnsureAndLockSlots(ctx, rsv.LotID, rsv.Window); err != nil {
			return err
		}
		if err := j.s.repo.DecSlots(ctx, rsv.LotID, rsv.Window); err != nil {
			return err
		}
		data := map[string]string{
			"reservation_id": rsv.PublicID.String(),
			"arrival_at":     lotTime(rsv.ArrivalAt),
		}
		// The policy captured at booking decides the block (FR-RES-18).
		pol, err := j.s.repo.PolicyByID(ctx, rsv.PolicyVersionID)
		if err != nil {
			return err
		}
		n, err := j.s.repo.CountNoShows(ctx, rsv.UserID, now, pol.NoShowWindowDays)
		if err != nil {
			return err
		}
		if n >= pol.NoShowThreshold {
			until, err := j.s.repo.BlockUser(ctx, rsv.UserID, now, pol.NoShowBlockDays)
			if err != nil {
				return err
			}
			data["blocked_until"] = lotTime(until)
		}
		if err := j.s.enqueue(ctx, rsv, notifications.TplReservationNoShow, data); err != nil {
			return fmt.Errorf("reservations: no-show email: %w", err)
		}
		expired = true
		return nil
	})
	return expired && err == nil, err
}
