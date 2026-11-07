-- Every driver-path query takes the owner id, so another driver's vehicle is
-- indistinguishable from a missing one.

-- name: LockVehicleOwner :one
-- Serializes one user's vehicle writes so the cap cannot be raced.
select id from users where id = @owner_id and deleted_at is null for update;

-- name: CountActiveVehiclesOwned :one
select count(*) from vehicles where user_id = @owner_id and deleted_at is null;

-- name: InsertVehicle :one
insert into vehicles (user_id, plate_normalized, plate_display, make, model, color, created_at, updated_at)
values (@owner_id, @plate_normalized, @plate_display, @make, @model, @color, @now, @now)
returning id, public_id, plate_normalized, plate_display, make, model, color, created_at, updated_at;

-- name: ListVehiclesOwned :many
select id, public_id, plate_normalized, plate_display, make, model, color, created_at, updated_at
from vehicles
where user_id = @owner_id and deleted_at is null
order by created_at, id;

-- name: GetVehicleOwned :one
select id, public_id, plate_normalized, plate_display, make, model, color, created_at, updated_at
from vehicles
where public_id = @public_id and user_id = @owner_id and deleted_at is null;

-- name: UpdateVehicleOwned :one
update vehicles
set plate_normalized = @plate_normalized,
    plate_display = @plate_display,
    make = @make,
    model = @model,
    color = @color,
    updated_at = @now
where public_id = @public_id and user_id = @owner_id and deleted_at is null
returning id, public_id, plate_normalized, plate_display, make, model, color, created_at, updated_at;

-- name: SoftDeleteVehicleOwned :execrows
update vehicles
set deleted_at = @now, updated_at = @now
where public_id = @public_id and user_id = @owner_id and deleted_at is null;

-- name: EraseVehiclesOfOwner :exec
-- Account deletion (spec 7.8): soft-delete and scrub, which frees the plates.
update vehicles
set plate_normalized = 'X' || right(upper(lpad(to_hex(id), 8, '0')), 8),
    plate_display = 'X' || right(upper(lpad(to_hex(id), 8, '0')), 8),
    make = '-', model = '-', color = '-',
    deleted_at = @now, updated_at = @now
where user_id = @owner_id and deleted_at is null;
