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
-- Every business timestamp is the server clock (§8.4). reservation_id is set
-- exactly for a reservation stay (stays_reservation_id_check).
insert into stays (lot_id, credential_kind, ticket_code_hash, employee_card_id, reservation_id, status,
                   tariff_version_id, accepted_at, entered_at, updated_at)
values (@lot_id, @credential_kind, sqlc.narg(ticket_code_hash), sqlc.narg(employee_card_id), sqlc.narg(reservation_id), @status,
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
-- Lot scoping is the caller's: stay_id must come from a lot-scoped FOR UPDATE
-- lookup (GetStayForUpdate and friends, design §8.2); the insert does not
-- re-check the lot. A succeeded or refunded payment needs succeeded_at.
insert into payments (kind, stay_id, method, amount_cents, status, idempotency_key, device_id, user_id, created_at, succeeded_at)
values (@kind, @stay_id, @method, @amount_cents, @status, @idempotency_key, sqlc.narg(device_id), sqlc.narg(user_id),
        sqlc.arg(now)::timestamptz, sqlc.narg(succeeded_at))
returning id;

-- name: PaymentTotals :one
-- What the stay has paid so far and when the last payment landed.
select coalesce(sum(p.amount_cents), 0)::bigint as paid_cents,
       max(p.succeeded_at)::timestamptz as last_succeeded_at
from payments p
join stays s on s.id = p.stay_id
where s.lot_id = @lot_id and p.stay_id = @stay_id and p.status = 'succeeded';

-- name: LockLotOccupancy :one
-- The entry decision's lock (design §9.2, M2C-29): only the occupancy row is
-- locked, so FK inserts referencing lots are not blocked.
select o.occupied, l.capacity, l.status
from lot_occupancy o
join lots l on l.id = o.lot_id
where o.lot_id = @lot_id
for update of o;

-- name: CountOutOfService :one
select count(*) from spaces where lot_id = @lot_id and status = 'out_of_service';

-- name: CountFreshPending :one
-- Accepted entries hold their space for 60 s until the barrier pass (M2C-30).
select count(*) from stays
where lot_id = @lot_id and status = 'entry_pending'
  and accepted_at > sqlc.arg(now)::timestamptz - interval '60 seconds';

-- name: ActivateStay :execrows
-- The entry pass (design §9.6). voided_at is kept on a late pass.
update stays
set status = 'active', entered_at = sqlc.arg(now)::timestamptz, updated_at = sqlc.arg(now)::timestamptz
where lot_id = @lot_id and id = @id and status in ('entry_pending', 'voided');

-- name: BumpOccupancy :one
-- Only under the lot lock (M2C-29); check (occupied >= 0) backs a decrement.
update lot_occupancy
set occupied = occupied + @delta, version = version + 1, updated_at = sqlc.arg(now)::timestamptz
where lot_id = @lot_id
returning version;

-- name: NotifyLotChanged :exec
-- Delivered on commit; the payload is lot_id:version and never leaves the
-- server (design §8.3).
select pg_notify('lot_changed', sqlc.arg(lot_id)::bigint || ':' || sqlc.arg(version)::bigint);

-- name: VoidUnpassed :execrows
-- Entries not passed within 60 s stop holding a space (M2C-30, design
-- §12.5). Batches of 200; occupancy is untouched, so nothing is notified.
-- Reservation stays are voided one by one with their reservation (M3B-60).
update stays
set status = 'voided', voided_at = sqlc.arg(now)::timestamptz, updated_at = sqlc.arg(now)::timestamptz
where id in (
    select id from stays
    where status = 'entry_pending' and accepted_at < sqlc.arg(now)::timestamptz - interval '60 seconds'
      and credential_kind <> 'reservation'
    order by accepted_at
    limit 200
    for update skip locked
);

-- name: SetStayPaid :execrows
-- A cash payment (design §10.3): paid_at and the exit window move together,
-- so a later overstay is billed from this payment (M2C-35).
update stays
set status = 'paid', paid_at = sqlc.arg(now)::timestamptz,
    exit_deadline_at = sqlc.arg(exit_deadline_at)::timestamptz, updated_at = sqlc.arg(now)::timestamptz
where lot_id = @lot_id and id = @id and status = 'active';

-- name: ReactivateStay :execrows
-- The lazy paid -> active transition once the exit window has passed
-- (M2C-35). exit_deadline_at is kept: it keys the overstay pricing. The stay
-- owes money again, so an exit authorization is cancelled.
update stays
set status = 'active', exit_authorized_at = null, updated_at = sqlc.arg(now)::timestamptz
where lot_id = @lot_id and id = @id and status = 'paid';

-- name: MarkStayLost :execrows
-- A lost ticket (design §10.2): the old code stops working and a paid stay
-- owes money again. Marking an already-lost stay again only rotates the
-- code, so a lost stay that paid keeps its status. paid_at and
-- exit_deadline_at are kept (O13). Any exit authorization is cancelled: the
-- stay may owe money again, and the driver presents the new code.
update stays
set lost_ticket = true, ticket_code_hash = @ticket_code_hash, exit_authorized_at = null,
    status = case when lost_ticket then status else 'active' end,
    updated_at = sqlc.arg(now)::timestamptz
where lot_id = @lot_id and id = @id and credential_kind = 'ticket' and status in ('active', 'paid');

-- name: GetCardByHash :one
-- Any status: a card revoked while its car is inside may still leave
-- through its open stay. Entry uses GetActiveCardByHash.
select id, public_id, status from employee_cards
where lot_id = @lot_id and card_uid_hash = @hash;

-- name: AuthorizeExit :execrows
-- An exit credential that opened (design §10.4): the zero-due decision is
-- taken now, and the exit pass trusts it for 60 s (M2C-32).
update stays
set exit_authorized_at = sqlc.arg(now)::timestamptz, updated_at = sqlc.arg(now)::timestamptz
where lot_id = @lot_id and id = @id and status in ('active', 'paid');

-- name: SetStayExited :execrows
-- The exit pass (design §10.5). An active stay hops active -> paid -> exited
-- here in one statement, paid by its zero payment at now; a paid stay keeps
-- the paid_at of its last payment.
update stays
set status = 'exited', exited_at = sqlc.arg(now)::timestamptz,
    paid_at = coalesce(paid_at, sqlc.arg(now)::timestamptz), updated_at = sqlc.arg(now)::timestamptz
where lot_id = @lot_id and id = @id and status in ('active', 'paid');

-- name: ClearExitAuthorization :execrows
-- An exit credential denied for payment cancels an earlier open, so the
-- exit pass cannot use it to skip the payment.
update stays
set exit_authorized_at = null, updated_at = sqlc.arg(now)::timestamptz
where lot_id = @lot_id and id = @id and status in ('active', 'paid');

-- name: GetReservationStay :one
-- The reservation's one stay (stays_reservation_key), unlocked: the QR entry
-- decides from it under the lot and reservation locks, and locks it only
-- after the slot loads (design 8.1). Lot-scoped (M2C-21).
select sqlc.embed(s), r.user_id as reservation_user_id
from stays s
join reservations r on r.id = s.reservation_id
where s.lot_id = @lot_id and s.reservation_id = @reservation_id;

-- name: GetReservationStayForUpdate :one
select sqlc.embed(s), r.user_id as reservation_user_id
from stays s
join reservations r on r.id = s.reservation_id
where s.lot_id = @lot_id and s.reservation_id = @reservation_id
for update of s;

-- name: ReopenReservationStay :execrows
-- A reservation QR re-presented after the void (M3B-24, M3B-60): the voided
-- reservation stay is reused. voided_at is kept.
update stays
set status = 'entry_pending', accepted_at = sqlc.arg(now)::timestamptz, updated_at = sqlc.arg(now)::timestamptz
where lot_id = @lot_id and id = @id and credential_kind = 'reservation' and status = 'voided';

-- name: RefreshStayAccepted :execrows
-- A reservation QR re-presented while its stay waits for the barrier pass
-- (M3B-24): the 60 s pending window starts again.
update stays
set accepted_at = sqlc.arg(now)::timestamptz, updated_at = sqlc.arg(now)::timestamptz
where lot_id = @lot_id and id = @id and status = 'entry_pending';

-- name: ReservationStaysToVoid :many
-- Unlocked candidates of the reservation void (design 12.7 step 1); each is
-- re-checked under its locks.
select id, lot_id, reservation_id::bigint as reservation_id from stays
where credential_kind = 'reservation' and status = 'entry_pending'
  and accepted_at < sqlc.arg(now)::timestamptz - interval '60 seconds'
order by accepted_at
limit 200;

-- name: GetReservationUserID :one
-- The owner of a reservation stay's reservation, read without a lock: the
-- user never changes (M3B-27 attributes the stay's payments to it).
select user_id from reservations where id = @id;

-- name: GetStayForUpdateByID :one
select * from stays where lot_id = @lot_id and id = @id for update;

-- name: VoidStay :execrows
-- The reservation void (M3B-60): one locked entry_pending stay.
update stays
set status = 'voided', voided_at = sqlc.arg(now)::timestamptz, updated_at = sqlc.arg(now)::timestamptz
where lot_id = @lot_id and id = @id and status = 'entry_pending';
