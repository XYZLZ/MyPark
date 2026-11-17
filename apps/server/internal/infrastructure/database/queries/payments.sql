-- name: InsertFeeAttempt :one
-- A pending reservation-fee attempt (design 9.3). The public id comes from Go
-- so the gateway key is set in the same insert; created_at is the business
-- time, not now() (M3B-53).
insert into payments (public_id, kind, method, amount_cents, status, idempotency_key, reservation_id, user_id,
                      client_key, request_fingerprint, card_brand, card_last4, created_at)
values (@public_id, 'reservation_fee', 'gateway_sim', @amount_cents, 'pending', @idempotency_key, @reservation_id, @user_id,
        @client_key, @request_fingerprint, @card_brand, @card_last4, @now)
returning id, public_id, status, coalesce(failure_code, '')::text as failure_code, idempotency_key;

-- name: GetFeeAttemptByClientKey :one
-- The replay lookup: the key is scoped to the reservation (M3B-7).
select id, public_id, status, coalesce(failure_code, '')::text as failure_code, idempotency_key, request_fingerprint
from payments
where reservation_id = @reservation_id and client_key = @client_key;

-- name: GetFeeByID :one
-- Unlocked: reservation_id and amount never change after the insert.
select id, public_id, status, coalesce(failure_code, '')::text as failure_code, idempotency_key, reservation_id, amount_cents
from payments
where id = @id and kind = 'reservation_fee';

-- name: FinalizeFeeSucceeded :execrows
-- Fenced on pending: the reconcile job and the pay request may both finish
-- the same attempt, and only the first one writes.
update payments
set status = 'succeeded', succeeded_at = @now, provider_ref = @provider_ref
where id = @id and kind = 'reservation_fee' and status = 'pending';

-- name: FinalizeFeeFailed :execrows
update payments
set status = 'failed', failed_at = @now, failure_code = @failure_code
where id = @id and kind = 'reservation_fee' and status = 'pending';

-- name: GetLiveFee :one
select id, public_id, status, coalesce(failure_code, '')::text as failure_code, idempotency_key, reservation_id, amount_cents
from payments
where reservation_id = @reservation_id and kind = 'reservation_fee' and status in ('pending', 'succeeded', 'refunded');

-- name: GetLatestFee :one
select id, public_id, status, coalesce(failure_code, '')::text as failure_code, idempotency_key, reservation_id, amount_cents
from payments
where reservation_id = @reservation_id and kind = 'reservation_fee'
order by created_at desc, id desc
limit 1;

-- name: InsertRefund :one
-- A pending full refund, due now (M3B-29). The gateway key is set in the same
-- insert from the public id generated in Go.
insert into refunds (public_id, payment_id, amount_cents, reason, status, idempotency_key, next_attempt_at, created_at)
values (@public_id, @payment_id, @amount_cents, @reason, 'pending', @idempotency_key, @now, @now)
returning id, public_id, payment_id, amount_cents, reason, status;

-- name: ListPendingFeesBefore :many
-- The reconcile job's work list (design 9.4): fee attempts still pending after
-- the charge timeout plus a margin. Unlocked on purpose: the gateway is called
-- with no transaction open, and Apply is fenced on pending.
select id, public_id, status, coalesce(failure_code, '')::text as failure_code, idempotency_key, reservation_id, amount_cents
from payments
where kind = 'reservation_fee' and status = 'pending' and created_at < @cutoff
order by created_at, id
limit sqlc.arg('batch');

-- name: LockReservationOfFee :one
-- The reservation lock a not-submitted mark takes before it touches the payment
-- (lock order: reservations before payments, design 8.1).
select r.id
from reservations r
where r.id = (select p.reservation_id from payments p where p.id = @payment_id and p.kind = 'reservation_fee')
for update of r;

-- name: FinalizeFeeNotSubmitted :execrows
-- Fenced on pending and on age (M3B-10): an attempt younger than @cutoff may
-- still have a charge in flight.
update payments
set status = 'failed', failed_at = @now, failure_code = 'not_submitted'
where id = @id and kind = 'reservation_fee' and status = 'pending' and created_at < @cutoff;

-- name: FinalizeFeeLateSuccess :execrows
-- A charge that landed after the job marked the attempt not_submitted (design
-- 9.4): failed -> succeeded, fenced on that exact state. The failed check
-- requires failed_at and failure_code to go with the status.
update payments
set status = 'succeeded', succeeded_at = @now, provider_ref = @provider_ref, failed_at = null, failure_code = null
where id = @id and kind = 'reservation_fee' and status = 'failed' and failure_code = 'not_submitted';

-- name: GetLatestRefund :one
-- The newest refund of a payment (the unique live index allows one pending or
-- succeeded; a failed one may precede it).
select id, public_id, payment_id, amount_cents, reason, status, succeeded_at
from refunds
where payment_id = @payment_id
order by created_at desc, id desc
limit 1;

-- name: ClaimDueRefunds :many
-- Design 12.5 step 1 (M3B-29): lease the due refunds. skip locked keeps two
-- runners apart while the claim runs; the lease (next_attempt_at moved ahead)
-- keeps them apart after it commits, and attempts+1 is the finalize fence.
with due as (
    select refunds.id from refunds
    where refunds.status = 'pending' and refunds.next_attempt_at <= @now
      and refunds.attempts < sqlc.arg('max_attempts')::smallint
    order by refunds.next_attempt_at, refunds.id
    limit sqlc.arg('batch')
    for update skip locked
)
update refunds r
set attempts = r.attempts + 1, next_attempt_at = @lease_until
from due, payments p, reservations rv
where r.id = due.id and r.status = 'pending' and p.id = r.payment_id and rv.id = p.reservation_id
returning r.id, r.public_id, r.payment_id, r.amount_cents, r.attempts::int as attempts,
          r.idempotency_key, p.idempotency_key as charge_key, p.public_id as payment_public_id,
          rv.id as reservation_id, rv.public_id as reservation_public_id, rv.lot_id;

-- name: LockPaymentForRefund :one
-- Lock order: payments before refunds (design 8.1).
select id from payments where id = @id for update;

-- name: FinalizeRefundSucceeded :execrows
update refunds
set status = 'succeeded', provider_ref = @provider_ref, succeeded_at = @now, last_error = null
where id = @id and status = 'pending' and attempts = @attempts;

-- name: MarkPaymentRefunded :execrows
update payments
set status = 'refunded', refunded_at = @now
where id = @id and kind = 'reservation_fee' and status = 'succeeded';

-- name: FinalizeRefundRetry :execrows
update refunds
set last_error = @last_error, next_attempt_at = @next_attempt_at
where id = @id and status = 'pending' and attempts = @attempts;

-- name: FinalizeRefundFailed :execrows
update refunds
set status = 'failed', failed_at = @now, last_error = @last_error
where id = @id and status = 'pending' and attempts = @attempts;

-- name: SweepStrandedRefunds :many
-- A refund left pending at the attempt limit whose lease has passed: its last
-- runner died before finalizing (ClaimDueRefunds never takes it again). Fail it,
-- fenced on pending and on that attempts value, and return it for the alert.
with stuck as (
    select refunds.id, refunds.attempts from refunds
    where refunds.status = 'pending' and refunds.attempts >= sqlc.arg('max_attempts')::smallint
      and refunds.next_attempt_at <= @now
    order by refunds.id
    limit sqlc.arg('batch')
    for update skip locked
)
update refunds r
set status = 'failed', failed_at = @now, last_error = coalesce(r.last_error, 'lease_expired')
from stuck, payments p, reservations rv
where r.id = stuck.id and r.status = 'pending' and r.attempts = stuck.attempts
  and p.id = r.payment_id and rv.id = p.reservation_id
returning r.id, r.public_id, r.payment_id, r.amount_cents, r.attempts::int as attempts,
          r.idempotency_key, p.idempotency_key as charge_key, p.public_id as payment_public_id,
          rv.id as reservation_id, rv.public_id as reservation_public_id, rv.lot_id;

-- name: ListReceiptsForUser :many
-- Receipts of one driver (design 7.10, M3B-37): every payment row with
-- user_id = me, newest first on (created_at, id). @has_id narrows to one public
-- id (the detail read); the cursor row is resolved by public id inside the
-- user's own rows. The reservation is the fee's own or the one of the stay a
-- parking fee paid; the latest refund is embedded. provider_ref and card data
-- beyond brand and last four are never selected.
select p.id, p.public_id, p.kind, p.method, p.amount_cents, p.status, p.card_brand, p.card_last4,
       p.failure_code, p.created_at, p.succeeded_at, p.refunded_at,
       rv.public_id as reservation_public_id,
       rf.public_id as refund_public_id, rf.status as refund_status, rf.amount_cents as refund_amount_cents,
       rf.reason as refund_reason, rf.succeeded_at as refund_succeeded_at
from payments p
left join stays s on s.id = p.stay_id
left join reservations rv on rv.id = coalesce(p.reservation_id, s.reservation_id)
left join refunds rf on rf.id = (
    select f.id from refunds f where f.payment_id = p.id order by f.created_at desc, f.id desc limit 1
)
where p.user_id = @user_id
  and (not @has_id::boolean or p.public_id = @public_id::uuid)
  and (not @has_cursor::boolean
       or (p.created_at, p.id) < (
           @cursor_created::timestamptz,
           coalesce((select c.id from payments c where c.public_id = @cursor_public_id::uuid and c.user_id = @user_id), 0)))
order by p.created_at desc, p.id desc
limit @row_limit::int;
