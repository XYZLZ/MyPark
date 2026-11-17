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
