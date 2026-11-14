-- name: UpdateSpaceFromSensor :one
-- A sensor never overrides an operator's out_of_service, and an unchanged
-- state writes nothing, so a row comes back only on a real change (design
-- §8.3). sensor_updated_at is the server clock (§8.4).
update spaces
set status = @status, sensor_updated_at = sqlc.arg(now)::timestamptz
where lot_id = @lot_id
  and code = @code
  and status <> 'out_of_service'
  and status <> @status
returning code;

-- name: SpaceExists :one
select exists (select 1 from spaces where lot_id = @lot_id and code = @code);

-- name: NotifySpaceChanged :exec
-- Delivered on commit; the payload is lot_id:code:state and never leaves the
-- server.
select pg_notify('space_changed', sqlc.arg(payload)::text);
