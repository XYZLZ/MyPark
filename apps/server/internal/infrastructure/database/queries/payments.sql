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
