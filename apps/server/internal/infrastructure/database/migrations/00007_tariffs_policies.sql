-- Versioned tariffs and booking policies (design M2 section 5.1, M2C-2).
-- Forward-only: no Down section.
--
-- Runs as mypark_migrator. The DML grants to mypark_app come from the default
-- privileges set in 00001_baseline.sql; both tables then have update, delete
-- and truncate revoked (M2C-42): a stay points at the version that priced it,
-- so a version never changes. A correction is a new row with a later
-- effective_from.

-- +goose Up
create table tariff_versions (
    id                    bigint generated always as identity primary key,
    lot_id                bigint not null references lots (id),
    effective_from        timestamptz not null,
    first_hour_cents      bigint not null check (first_hour_cents >= 0),
    additional_hour_cents bigint not null check (additional_hour_cents >= 0),
    exit_grace_min        int not null check (exit_grace_min between 0 and 120),
    post_payment_exit_min int not null check (post_payment_exit_min between 1 and 120),
    daily_cap_cents       bigint not null check (daily_cap_cents >= first_hour_cents),
    lost_ticket_cents     bigint not null check (lost_ticket_cents >= 0),
    reservation_fee_cents bigint not null check (reservation_fee_cents >= 0),
    created_by            bigint references users (id),
    created_at            timestamptz not null default now(),
    -- Also the lot_id FK index and the "current version" lookup.
    unique (lot_id, effective_from)
);

create index tariff_versions_created_by_idx on tariff_versions (created_by);

create table policy_versions (
    id                     bigint generated always as identity primary key,
    lot_id                 bigint not null references lots (id),
    effective_from         timestamptz not null,
    max_reservable_pct     int not null check (max_reservable_pct between 0 and 100),
    booking_horizon_days   int not null check (booking_horizon_days > 0),
    arrival_grace_min      int not null check (arrival_grace_min > 0),
    early_arrival_min      int not null check (early_arrival_min > 0),
    payment_timeout_min    int not null check (payment_timeout_min > 0),
    max_active_per_user    int not null check (max_active_per_user > 0),
    slot_min               int not null check (slot_min > 0),
    noshow_block_threshold int not null check (noshow_block_threshold > 0),
    noshow_window_days     int not null check (noshow_window_days > 0),
    noshow_block_days      int not null check (noshow_block_days > 0),
    created_by             bigint references users (id),
    created_at             timestamptz not null default now(),
    unique (lot_id, effective_from)
);

create index policy_versions_created_by_idx on policy_versions (created_by);

-- Append-only for the application role (default privileges granted these).
revoke update, delete, truncate on tariff_versions, policy_versions from mypark_app;
