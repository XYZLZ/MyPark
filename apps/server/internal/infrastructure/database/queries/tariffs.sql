-- name: GetTariffVersionAt :one
-- The version in force at @at: the latest one already effective. Served by
-- the unique (lot_id, effective_from) index.
select *
from tariff_versions
where lot_id = @lot_id and effective_from <= @at
order by effective_from desc
limit 1;

-- name: GetTariffVersionByID :one
select * from tariff_versions
where id = @id;

-- name: InsertTariffVersion :exec
insert into tariff_versions (
    lot_id, effective_from, first_hour_cents, additional_hour_cents, exit_grace_min,
    post_payment_exit_min, daily_cap_cents, lost_ticket_cents, reservation_fee_cents
)
values (
    @lot_id, @effective_from, @first_hour_cents, @additional_hour_cents, @exit_grace_min,
    @post_payment_exit_min, @daily_cap_cents, @lost_ticket_cents, @reservation_fee_cents
)
on conflict (lot_id, effective_from) do nothing;

-- name: InsertPolicyVersion :exec
insert into policy_versions (
    lot_id, effective_from, max_reservable_pct, booking_horizon_days, arrival_grace_min,
    early_arrival_min, payment_timeout_min, max_active_per_user, slot_min,
    noshow_block_threshold, noshow_window_days, noshow_block_days
)
values (
    @lot_id, @effective_from, @max_reservable_pct, @booking_horizon_days, @arrival_grace_min,
    @early_arrival_min, @payment_timeout_min, @max_active_per_user, @slot_min,
    @noshow_block_threshold, @noshow_window_days, @noshow_block_days
)
on conflict (lot_id, effective_from) do nothing;
