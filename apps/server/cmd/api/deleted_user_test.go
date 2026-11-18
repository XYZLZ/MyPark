package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// deleteOwner deletes the account that owns the reservation the way
// DELETE /v1/me does (spec 7.8), in one transaction: profile, auth columns,
// vehicles and the outbox scrub.
func (e *feeEnv) deleteOwner(rsvID string) {
	e.t.Helper()
	var userID int64
	if err := e.db.App.QueryRow(e.t.Context(), `select user_id from reservations where public_id = $1`, rsvID).Scan(&userID); err != nil {
		e.t.Fatal(err)
	}
	now := pgtype.Timestamptz{Time: e.clk.Now(), Valid: true}
	err := database.NewTxRunner(e.db.App).InTx(e.t.Context(), func(ctx context.Context) error {
		tx, err := database.MustTx(ctx)
		if err != nil {
			return err
		}
		q := dbgen.New(tx)
		if _, err := q.AnonymizeUserProfile(ctx, dbgen.AnonymizeUserProfileParams{Now: now, ID: userID}); err != nil {
			return err
		}
		if _, err := q.EraseAuthUser(ctx, dbgen.EraseAuthUserParams{Now: now, ID: userID}); err != nil {
			return err
		}
		if err := q.EraseVehiclesOfOwner(ctx, dbgen.EraseVehiclesOfOwnerParams{Now: now, OwnerID: userID}); err != nil {
			return err
		}
		return q.ScrubPendingOutboxCodesForUser(ctx, pgtype.Int8{Int64: userID, Valid: true})
	})
	if err != nil {
		e.t.Fatalf("delete account: %v", err)
	}
}

// noMail fails when an email with the dedupe key was queued.
func (e *feeEnv) noMail(dedupeKey string) {
	e.t.Helper()
	if n := e.count(`select count(*) from outbox_messages where dedupe_key = $1`, dedupeKey); n != 0 {
		e.t.Errorf("%s: %d outbox rows, want none for a deleted user", dedupeKey, n)
	}
}

// C1: a deleted driver has no one to write to, so the email is skipped and
// the money and state change it belongs to still commits.
func TestDeletedUser_ReconcileSettlesPendingFee(t *testing.T) {
	for _, tc := range []struct {
		name, status, mail string
		at                 func(e *feeEnv)
	}{
		{"confirm", "confirmed", "reservation_confirmed:", func(e *feeEnv) { e.clk.Advance(61 * time.Second) }},
		{"late", "expired_unpaid", "reservation_cancelled:", func(e *feeEnv) { e.clk.Set(time.Date(2026, 12, 1, 15, 25, 0, 0, time.UTC)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newFeeEnv(t)
			id, _ := e.payTimeout()
			e.deleteOwner(id)
			rec, _ := e.reconciler()
			tc.at(e)

			if n, err := rec.Run(t.Context(), e.clk.Now()); err != nil || n != 1 {
				t.Fatalf("Run = %d, %v; want 1", n, err)
			}
			if got := e.status(id); got != tc.status {
				t.Errorf("reservation = %s, want %s", got, tc.status)
			}
			if c := e.count(`select count(*) from payments p join reservations r on r.id = p.reservation_id
				where r.public_id = $1 and p.status = 'succeeded'`, id); c != 1 {
				t.Error("the payment must be succeeded")
			}
			if tc.status == "expired_unpaid" {
				if c := e.count(`select count(*) from refunds where status = 'pending' and reason = 'facility_unavailable'`); c != 1 {
					t.Error("want one pending facility_unavailable refund")
				}
			}
			e.noMail(tc.mail + id)
			testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
		})
	}
}

func TestDeletedUser_QueuedRefundSucceeds(t *testing.T) {
	e := newFeeEnv(t)
	id, _ := e.cancelled()
	e.deleteOwner(id)

	if n, err := e.processor(e.simGateway(&refundGateway{})).Run(t.Context(), e.clk.Now()); err != nil || n != 1 {
		t.Fatalf("refund Run = %d, %v; want 1", n, err)
	}
	if c := e.count(`select count(*) from refunds where status = 'succeeded'`); c != 1 {
		t.Error("want the refund succeeded")
	}
	if c := e.count(`select count(*) from payments where status = 'refunded'`); c != 1 {
		t.Error("want the payment refunded")
	}
	if c := e.count(`select count(*) from outbox_messages where template = 'refund_succeeded'`); c != 0 {
		t.Errorf("refund_succeeded emails = %d, want none for a deleted user", c)
	}
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestDeletedUser_NoShowExpires(t *testing.T) {
	e := newFeeEnv(t)
	id, _ := e.paid(arrival1502)
	e.deleteOwner(id)
	e.clk.Set(time.Date(2026, 12, 1, 15, 26, 0, 0, time.UTC)) // past upper(window)

	if n := e.expireNoShows(); n != 1 {
		t.Fatalf("expired = %d, want 1", n)
	}
	if got := e.status(id); got != "expired_no_show" {
		t.Errorf("reservation = %s, want expired_no_show", got)
	}
	e.noMail("reservation_no_show:" + id)
	testsupport.AssertSlotInvariant(t, e.db.App, e.lotID)
}

func TestDeletedUser_PhysicallyFullCancels(t *testing.T) {
	e := newQREnv(t)
	id, _, qr := e.confirmed(arrivalInWindow)
	e.deleteOwner(id)
	testsupport.FillLot(t, e.db, e.lotID, 280, e.clk.Now())

	wantQRDeny(t, e.present(qr), "gate.lot_physically_full")
	if n := e.count(`select count(*) from reservations where public_id = $1 and status = 'cancelled' and cancelled_by = 'system'`, id); n != 1 {
		t.Error("want the reservation cancelled by system")
	}
	if n := e.count(`select count(*) from refunds where status = 'pending' and reason = 'facility_unavailable'`); n != 1 {
		t.Error("want one pending facility_unavailable refund")
	}
	e.noMail("reservation_cancelled:" + id)
	e.invariants()
}
