-- name: InsertSimCharge :one
-- Records a charge once per idempotency key. No row back means the key
-- already exists: the caller re-reads it (GetSimCharge).
insert into sim_gateway_charges (idempotency_key, amount_cents, outcome, decline_code, card_brand, card_last4, provider_ref, delayed, created_at)
values (@idempotency_key, @amount_cents, @outcome, sqlc.narg('decline_code'), @card_brand, @card_last4, @provider_ref, @delayed, @now)
on conflict (idempotency_key) do nothing
returning idempotency_key, outcome, decline_code, provider_ref, delayed;

-- name: GetSimCharge :one
select idempotency_key, outcome, decline_code, provider_ref, delayed
from sim_gateway_charges
where idempotency_key = @idempotency_key;

-- name: RefundSimCharge :one
-- Full refund, once per charge: only a succeeded charge of the same amount, and
-- only with no refund yet or the same refund key. A repeat keeps the first
-- provider ref and timestamp.
update sim_gateway_charges
set refund_idempotency_key = @refund_key,
    refund_provider_ref    = coalesce(refund_provider_ref, @refund_provider_ref),
    refunded_at            = coalesce(refunded_at, @now::timestamptz)
where idempotency_key = @charge_key
  and outcome = 'succeeded'
  and amount_cents = @amount_cents
  and (refund_idempotency_key is null or refund_idempotency_key = @refund_key)
returning refund_provider_ref::text as refund_provider_ref;
