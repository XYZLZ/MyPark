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

-- name: GetActiveCardByHash :one
-- Lot-scoped (M2C-21): a card of another lot is unknown here.
select id, public_id from employee_cards
where lot_id = @lot_id and card_uid_hash = @hash and status = 'active';

-- name: InsertEmployeeCard :execrows
-- Idempotent seed: the hash is unique across lots, and an existing card is
-- left as it is. Returns the rows inserted (0 or 1).
insert into employee_cards (lot_id, card_uid_hash, holder_name, employee_code, status, revoked_at)
values (@lot_id, @card_uid_hash, @holder_name, @employee_code, @status, sqlc.narg(revoked_at))
on conflict (card_uid_hash) do nothing;

-- name: InsertStay :one
-- Every business timestamp is the server clock (§8.4).
insert into stays (lot_id, credential_kind, ticket_code_hash, employee_card_id, status,
                   tariff_version_id, accepted_at, entered_at, updated_at)
values (@lot_id, @credential_kind, sqlc.narg(ticket_code_hash), sqlc.narg(employee_card_id), @status,
        @tariff_version_id, sqlc.arg(now)::timestamptz, sqlc.narg(entered_at), sqlc.arg(now)::timestamptz)
returning id, public_id;

-- name: GetStayForUpdate :one
select * from stays where lot_id = @lot_id and public_id = @public_id for update;

-- name: GetStayByTicketHashForUpdate :one
select * from stays where lot_id = @lot_id and ticket_code_hash = @ticket_code_hash for update;

-- name: GetOpenStayByCardForUpdate :one
select * from stays
where lot_id = @lot_id and employee_card_id = @employee_card_id
  and status in ('entry_pending', 'active', 'paid')
for update;

-- name: InsertPayment :one
insert into payments (kind, stay_id, method, amount_cents, status, idempotency_key, device_id, created_at, succeeded_at)
values (@kind, @stay_id, @method, @amount_cents, @status, @idempotency_key, sqlc.narg(device_id),
        sqlc.arg(now)::timestamptz, sqlc.narg(succeeded_at))
returning id;

-- name: PaymentTotals :one
-- What the stay has paid so far and when the last payment landed.
select coalesce(sum(p.amount_cents), 0)::bigint as paid_cents,
       max(p.succeeded_at)::timestamptz as last_succeeded_at
from payments p
join stays s on s.id = p.stay_id
where s.lot_id = @lot_id and p.stay_id = @stay_id and p.status = 'succeeded';
