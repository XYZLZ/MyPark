-- name: GetLotForReservation :one
-- Unlocked read for the create and quote checks: the lot and its effective
-- capacity inputs (design s8.3).
select l.id, l.public_id, l.status, l.capacity,
       (select count(*) from spaces s where s.lot_id = l.id and s.status = 'out_of_service')::int as out_of_service
from lots l
where l.public_id = @public_id;

-- name: GetPolicyVersionAt :one
-- The policy in force at @at: the latest one already effective.
select *
from policy_versions
where lot_id = @lot_id and effective_from <= @at
order by effective_from desc
limit 1;

-- name: GetVehicleForShare :one
-- A live vehicle of the owner, share-locked so it cannot be deleted or
-- re-owned before the reservation commits (M3B-31).
select id, public_id, plate_display, make, model, color
from vehicles
where public_id = @public_id and user_id = @user_id and deleted_at is null
for share;

-- name: GetReservationBlockedUntil :one
select reservation_blocked_until from users where id = @user_id;

-- name: HasActiveReservation :one
select exists (
    select 1 from reservations
    where user_id = @user_id and status in ('pending_payment', 'confirmed')
) as active;

-- name: InsertReservation :one
insert into reservations (
    user_id, vehicle_id, lot_id, arrival_at, hold_window, status, fee_cents,
    tariff_version_id, policy_version_id, payment_due_at, created_at, updated_at
)
values (
    @user_id, @vehicle_id, @lot_id, @arrival_at, tstzrange(@window_start::timestamptz, @window_end::timestamptz, '[)'),
    'pending_payment', @fee_cents, @tariff_version_id, @policy_version_id, @payment_due_at, @now, @now
)
returning id, public_id, arrival_at, status, fee_cents, payment_due_at, created_at;

-- name: EnsureSlotLoads :exec
-- Creates the missing slot rows of the window (design s8.3). Rows are inserted
-- in ascending order, so two overlapping windows cannot deadlock here.
insert into reservation_slot_loads (lot_id, slot_start, held)
select @lot_id::bigint, s, 0
from generate_series(@window_start::timestamptz, @window_last::timestamptz, '5 minutes'::interval) s
on conflict do nothing;

-- name: LockSlotLoads :many
-- Locks the window's slot rows in ascending slot_start order (M3B-14).
select held from reservation_slot_loads
where lot_id = @lot_id and slot_start >= @window_start and slot_start < @window_end
order by slot_start
for update;

-- name: ReadSlotLoads :many
-- Unlocked, for the quote (M3B-57). Missing rows are not returned.
select held from reservation_slot_loads
where lot_id = @lot_id and slot_start >= @window_start and slot_start < @window_end
order by slot_start;

-- name: IncSlotLoads :exec
update reservation_slot_loads set held = held + 1
where lot_id = @lot_id and slot_start >= @window_start and slot_start < @window_end;

-- name: DecSlotLoads :exec
-- The check (held >= 0) catches a double release.
update reservation_slot_loads set held = held - 1
where lot_id = @lot_id and slot_start >= @window_start and slot_start < @window_end;

-- name: CountEffectiveHolds :one
-- Confirmed reservations whose hold window contains @now (design s8.4).
select count(*)::int from reservations
where lot_id = @lot_id and status = 'confirmed' and hold_window @> @now::timestamptz;


-- name: CountUpcomingArrivals :one
-- Confirmed arrivals in (@now, @until] (design s8.4).
select count(*)::int from reservations
where lot_id = @lot_id and status = 'confirmed' and arrival_at > @now and arrival_at <= @until;


-- name: VehicleHasActiveReservation :one
select exists (
    select 1 from reservations
    where vehicle_id = @vehicle_id and status in ('pending_payment', 'confirmed', 'checked_in')
) as active;

-- name: GetReservationForUser :one
-- One reservation of the user with its vehicle (live or deleted) and lot.
-- Another user's id is simply not found (M3B-33).
select r.id, r.public_id, r.vehicle_id, r.arrival_at,
       lower(r.hold_window)::timestamptz as window_start, upper(r.hold_window)::timestamptz as window_end,
       r.status, r.fee_cents, r.payment_due_at, r.created_at, r.confirmed_at, r.cancelled_at,
       r.cancelled_by, r.refund_reason, r.checked_in_at, r.completed_at, r.expired_at,
       l.public_id as lot_public_id,
       v.public_id as vehicle_public_id, v.plate_display, v.make, v.model, v.color
from reservations r
join lots l on l.id = r.lot_id
join vehicles v on v.id = r.vehicle_id
where r.public_id = @public_id and r.user_id = @user_id;


-- name: ListReservationsForUser :many
-- Keyset page, newest arrival first (M3B-36). @scope is '' (all), 'active' or
-- 'past'. The cursor row is resolved by public id inside the user's own rows.
select r.id, r.public_id, r.vehicle_id, r.arrival_at,
       lower(r.hold_window)::timestamptz as window_start, upper(r.hold_window)::timestamptz as window_end,
       r.status, r.fee_cents, r.payment_due_at, r.created_at, r.confirmed_at, r.cancelled_at,
       r.cancelled_by, r.refund_reason, r.checked_in_at, r.completed_at, r.expired_at,
       l.public_id as lot_public_id,
       v.public_id as vehicle_public_id, v.plate_display, v.make, v.model, v.color
from reservations r
join lots l on l.id = r.lot_id
join vehicles v on v.id = r.vehicle_id
where r.user_id = @user_id
  and (@scope::text = ''
       or (r.status in ('pending_payment', 'confirmed', 'checked_in')) = (@scope::text = 'active'))
  and (not @has_cursor::boolean
       or (r.arrival_at, r.id) < (
           @cursor_arrival::timestamptz,
           coalesce((select c.id from reservations c where c.public_id = @cursor_public_id::uuid and c.user_id = @user_id), 0)))
order by r.arrival_at desc, r.id desc
limit @row_limit::int;


-- name: LockReservationForUser :one
-- The same row as GetReservationForUser, with the reservation locked for
-- update (design s8.1: reservations before payments).
select r.id, r.public_id, r.vehicle_id, r.arrival_at,
       lower(r.hold_window)::timestamptz as window_start, upper(r.hold_window)::timestamptz as window_end,
       r.status, r.fee_cents, r.payment_due_at, r.created_at, r.confirmed_at, r.cancelled_at,
       r.cancelled_by, r.refund_reason, r.checked_in_at, r.completed_at, r.expired_at,
       l.public_id as lot_public_id,
       v.public_id as vehicle_public_id, v.plate_display, v.make, v.model, v.color
from reservations r
join lots l on l.id = r.lot_id
join vehicles v on v.id = r.vehicle_id
where r.public_id = @public_id and r.user_id = @user_id
for update of r;


-- name: SetReservationVehicle :exec
update reservations set vehicle_id = @vehicle_id, updated_at = @now where id = @id;


-- name: GetReservationForApply :one
-- Unlocked, by internal id: Apply decides from the window whether it takes the
-- lot lock before it locks the reservation (design 8.1). The window never changes.
select r.id, r.public_id, r.user_id, r.lot_id, r.status,
       lower(r.hold_window)::timestamptz as window_start, upper(r.hold_window)::timestamptz as window_end
from reservations r
where r.id = @id;

-- name: LockReservationForApply :one
-- Joins the vehicle for the confirmation email; only the reservation row is locked.
select r.id, r.public_id, r.user_id, r.lot_id, r.status,
       lower(r.hold_window)::timestamptz as window_start, upper(r.hold_window)::timestamptz as window_end,
       r.arrival_at, r.fee_cents, v.plate_display
from reservations r
join vehicles v on v.id = r.vehicle_id
where r.id = @id
for update of r;

-- name: ConfirmReservation :execrows
-- The pay confirm (design 9.4 step 3): a fresh qr_nonce invalidates any older token.
update reservations
set status = 'confirmed', confirmed_at = @now, qr_nonce = @qr_nonce, updated_at = @now
where id = @id and status = 'pending_payment';

-- name: ExpireReservationLate :execrows
-- Late success (design 9.4 step 4): the reservation never became confirmed.
update reservations
set status = 'expired_unpaid', expired_at = @now, refund_reason = @refund_reason, updated_at = @now
where id = @id and status = 'pending_payment';

-- name: GetReservationQR :one
-- What the QR token is built from; another driver's id is simply not found (M3B-33).
select r.public_id, r.status, r.qr_nonce,
       lower(r.hold_window)::timestamptz as window_start, upper(r.hold_window)::timestamptz as window_end
from reservations r
where r.public_id = @public_id and r.user_id = @user_id;

-- name: CancelReservation :execrows
-- The driver cancel (design 7.8). A pending_payment reservation has no refund
-- reason; a confirmed one records why the fee is or is not returned.
update reservations
set status = 'cancelled', cancelled_at = @now, cancelled_by = @cancelled_by,
    refund_reason = sqlc.narg('refund_reason'), updated_at = @now
where id = @id and status in ('pending_payment', 'confirmed');

-- name: GetReservationForQR :one
-- The QR entry's lookup before any lock (design 11.1 step 1): lot-scoped, so
-- another lot's reservation is simply not found.
select r.id, r.public_id, r.qr_nonce
from reservations r
where r.lot_id = @lot_id and r.public_id = @public_id;

-- name: LockReservationForEntry :one
-- Taken after the lot lock (design 8.1, QR entry); the caller re-checks the nonce.
select r.id, r.public_id, r.user_id, r.tariff_version_id, r.status, r.qr_nonce,
       lower(r.hold_window)::timestamptz as window_start, upper(r.hold_window)::timestamptz as window_end
from reservations r
where r.lot_id = @lot_id and r.id = @id
for update;

-- name: CheckInReservation :execrows
-- A valid QR inside the window with room (design 11.1).
update reservations
set status = 'checked_in', checked_in_at = @now, updated_at = @now
where id = @id and status = 'confirmed';
