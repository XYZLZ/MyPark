-- name: GetDeviceByPublicID :one
select * from devices
where public_id = @public_id;

-- name: TouchDeviceLastSeen :exec
-- Throttled to one write per device per 30 s so heartbeats do not churn the row.
update devices
set last_seen_at = sqlc.arg(now)::timestamptz
where id = @id
  and (last_seen_at is null or last_seen_at < sqlc.arg(now)::timestamptz - interval '30 seconds');

-- name: GetFleetDevice :one
select id, secret_ciphertext, secret_key_id
from devices
where public_id = @public_id;

-- name: InsertFleetDevice :execrows
insert into devices (public_id, lot_id, kind, name, secret_ciphertext, secret_key_id)
values (@public_id, @lot_id, @kind, @name, @secret_ciphertext, @secret_key_id)
on conflict (public_id) do nothing;

-- name: UpdateDeviceSecret :exec
update devices
set secret_ciphertext = @secret_ciphertext, secret_key_id = @secret_key_id
where id = @id;

-- name: GetDeviceEvent :one
select * from device_events
where device_id = @device_id and event_id = @event_id;

-- name: InsertDeviceEvent :exec
-- received_at takes its default, the database clock (now()), so the retention
-- floor follows one clock and a caller cannot back-date evidence.
insert into device_events (lot_id, device_id, event_id, type, occurred_at, body_sha256, payload, response)
values (@lot_id, @device_id, @event_id, @type, @occurred_at, @body_sha256, @payload, @response);

-- name: PurgeDeviceEvents :one
select purge_device_events(@batch)::bigint;
