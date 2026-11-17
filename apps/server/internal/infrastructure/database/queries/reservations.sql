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
