package reservations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
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

// reminderLead is how long before arrival the reminder goes out (M3B-41).
const reminderLead = 30 * time.Minute

// Reminders enqueues the reservation_reminder email for every confirmed
// reservation arriving within the next 30 minutes that was booked at least 30
// minutes ahead (a later booking already has its confirmation email), and
// returns how many it queued (FR-NOTIF-1, M3B-41). The outbox dedupe key
// reservation_reminder:<public_id> makes a repeated or concurrent run harmless,
// and the candidate query skips reservations already reminded. A reservation
// cancelled between the list and the enqueue may still get one reminder; the
// window is milliseconds and the mail names its arrival time.
func (j *Jobs) Reminders(ctx context.Context, now time.Time) (int, error) {
	var sent int
	var errs []error
	var after int64
	for {
		rows, err := j.s.repo.ListDueReminders(ctx, now, after, expiryBatch)
		if err != nil {
			return sent, errors.Join(append(errs, err)...)
		}
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return sent, errors.Join(append(errs, err)...)
			}
			if err := j.remind(ctx, row); err != nil {
				j.s.log.ErrorContext(ctx, "reservations: reminder failed", "reservation_row", row.ID, "err", err)
				errs = append(errs, err)
				continue
			}
			sent++
		}
		if len(rows) < expiryBatch {
			return sent, errors.Join(errs...)
		}
		after = rows[len(rows)-1].ID
	}
}

func (j *Jobs) remind(ctx context.Context, row ReminderRow) error {
	return j.s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := j.s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		rsv := ApplyRow{ID: row.ID, UserID: row.UserID, PublicID: row.PublicID, Window: row.Window}
		return j.s.enqueue(ctx, rsv, notifications.TplReservationReminder, map[string]string{
			"reservation_id": row.PublicID.String(),
			"arrival_at":     lotTime(row.ArrivalAt),
			"window":         lotTime(row.Window.Start) + " - " + lotTime(row.Window.End),
		})
	})
}

// HoldBoundary publishes the effective-hold changes that happen with no
// transaction: a hold window opening or closing as time passes (M3B-39,
// design 12.3). Transactions that change holds at their own now bump the lot
// version themselves (M3B-13).
//
// It remembers, per lot, the effective-hold count it last published, in
// memory. On its first tick for a lot after the process starts, or when the
// count differs, it bumps lot_occupancy.version and NOTIFYs in a short
// transaction that locks only lot_occupancy. Comparing counts skips a tick
// where one window ends as another starts. Worker replicas each keep their
// own memory, so two of them may both bump for one change: the second push is
// a newer snapshot with the same content, which the stream contract allows.
// A HoldBoundary is for one goroutine, as the job runner calls it.
type HoldBoundary struct {
	pool  *pgxpool.Pool
	holds Holds
	tx    *database.TxRunner
	log   *slog.Logger
	last  map[int64]int
}

// NewHoldBoundary returns the boundary job over pool.
func NewHoldBoundary(pool *pgxpool.Pool, log *slog.Logger) *HoldBoundary {
	return &HoldBoundary{pool: pool, holds: NewHolds(pool), tx: database.NewTxRunner(pool), log: log, last: map[int64]int{}}
}

// Tick publishes the lots whose effective holds at now differ from the last
// value it published, and returns how many it bumped. A failing lot is logged
// and the others still run; the errors are joined.
func (b *HoldBoundary) Tick(ctx context.Context, now time.Time) (bumped int, err error) {
	ids, err := dbgen.New(b.pool).ListLotIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("reservations: list lots: %w", err)
	}
	var errs []error
	for _, id := range ids {
		n, err := b.holds.EffectiveHolds(ctx, id, now)
		if err == nil {
			if prev, seen := b.last[id]; seen && prev == n {
				continue
			}
			err = b.publish(ctx, id, now)
		}
		if err != nil {
			b.log.ErrorContext(ctx, "reservations: hold boundary notify failed", "lot_row", id, "err", err)
			errs = append(errs, err)
			continue
		}
		b.last[id] = n
		bumped++
	}
	return bumped, errors.Join(errs...)
}

// publish bumps the lot version (occupancy unchanged) and notifies on commit.
func (b *HoldBoundary) publish(ctx context.Context, lotID int64, now time.Time) error {
	return b.tx.InTx(ctx, func(ctx context.Context) error {
		conn := database.Conn(ctx, b.pool)
		if _, err := conn.Exec(ctx, "set local lock_timeout = '1s'"); err != nil {
			return fmt.Errorf("reservations: set lock timeout: %w", err)
		}
		q := dbgen.New(conn)
		version, err := q.BumpOccupancy(ctx, dbgen.BumpOccupancyParams{Delta: 0, Now: tstz(now), LotID: lotID})
		if err != nil {
			return fmt.Errorf("reservations: bump lot version: %w", err)
		}
		if err := q.NotifyLotChanged(ctx, dbgen.NotifyLotChangedParams{LotID: lotID, Version: version}); err != nil {
			return fmt.Errorf("reservations: notify lot changed: %w", err)
		}
		return nil
	})
}
