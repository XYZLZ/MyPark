-- Reservations, slot loads and the stay link (design M3 section 6.1,
-- FR-RES-3, FR-RES-17, M3B-17 to M3B-24). Forward-only: no Down section.
--
-- Runs as mypark_migrator. The DML grants to mypark_app come from the default
-- privileges set in 00001_baseline.sql. btree_gist is enabled there too.

-- +goose Up
-- Written only by the no-show job (M3B-40).
alter table users add column reservation_blocked_until timestamptz;

create table reservations (
    id                bigint generated always as identity primary key,
    public_id         uuid not null unique default uuidv7(),
    user_id           bigint not null references users (id),
    vehicle_id        bigint not null references vehicles (id),
    lot_id            bigint not null references lots (id),
    -- 5-minute aligned by the service (M3B-17).
    arrival_at        timestamptz not null,
    -- Half-open '[)' (M3B-18).
    hold_window       tstzrange not null,
    status            text not null check (status in ('pending_payment', 'confirmed', 'checked_in', 'completed',
                                                      'cancelled', 'expired_unpaid', 'expired_no_show')),
    fee_cents         bigint not null check (fee_cents >= 0),
    -- Captured at booking (FR-RES-18).
    tariff_version_id bigint not null references tariff_versions (id),
    policy_version_id bigint not null references policy_versions (id),
    -- Set at confirmation (M3B-21).
    qr_nonce          bytea check (octet_length(qr_nonce) = 16),
    -- M3B-19.
    payment_due_at    timestamptz not null,
    created_at        timestamptz not null default now(),
    updated_at        timestamptz not null,
    confirmed_at      timestamptz,
    cancelled_at      timestamptz,
    cancelled_by      text check (cancelled_by in ('driver', 'admin', 'system')),
    refund_reason     text check (refund_reason in ('cancelled_before_arrival', 'none_after_arrival',
                                                    'facility_unavailable', 'facility_closed', 'admin_override')),
    checked_in_at     timestamptz,
    completed_at      timestamptz,
    expired_at        timestamptz,
    constraint reservations_window_check check (
        lower_inc(hold_window) and not upper_inc(hold_window)
        and lower(hold_window) < arrival_at and arrival_at < upper(hold_window)),
    constraint reservations_confirmed_check check (
        status not in ('confirmed', 'checked_in', 'completed') or (confirmed_at is not null and qr_nonce is not null)),
    constraint reservations_cancelled_check check (
        (status = 'cancelled') = (cancelled_at is not null and cancelled_by is not null)),
    constraint reservations_checked_in_check check (status not in ('checked_in', 'completed') or checked_in_at is not null),
    constraint reservations_completed_check check (status <> 'completed' or completed_at is not null),
    constraint reservations_expired_check check (
        (status in ('expired_unpaid', 'expired_no_show')) = (expired_at is not null)),
    -- A refund reason exists only on a cancellation or a late-success expiry (M3B-10). Not named
    -- reservations_refund_reason_check: that is the inline column check's generated name.
    constraint reservations_refund_reason_status_check check (
        refund_reason is null or status in ('cancelled', 'expired_unpaid'))
);

-- FR-RES-3: one active reservation per driver (M3B-48).
create unique index reservations_user_active_key on reservations (user_id)
    where status in ('pending_payment', 'confirmed');
-- "My reservations" keyset (M3B-36); also the user_id FK index.
create index reservations_user_arrival_idx on reservations (user_id, arrival_at desc, id desc);
-- Lookups by lot and status; also the lot_id FK index. Reminders, upcoming arrivals.
create index reservations_lot_status_arrival_idx on reservations (lot_id, status, arrival_at);
-- Effective holds at a point in time.
create index reservations_confirmed_window_idx on reservations using gist (lot_id, hold_window)
    where status = 'confirmed';
-- Unpaid expiry job.
create index reservations_payment_due_idx on reservations (payment_due_at) where status = 'pending_payment';
-- No-show job.
create index reservations_window_end_idx on reservations ((upper(hold_window))) where status = 'confirmed';
-- No-show block count.
create index reservations_no_show_idx on reservations (user_id, expired_at) where status = 'expired_no_show';
-- FK indexes.
create index reservations_vehicle_id_idx on reservations (vehicle_id);
create index reservations_tariff_version_id_idx on reservations (tariff_version_id);
create index reservations_policy_version_id_idx on reservations (policy_version_id);

create table reservation_slot_loads (
    lot_id     bigint not null references lots (id),
    slot_start timestamptz not null,
    held       int not null default 0 check (held >= 0),
    -- Also the lot_id FK index.
    primary key (lot_id, slot_start),
    constraint reservation_slot_loads_aligned_check check (extract(epoch from slot_start)::bigint % 300 = 0)
);

alter table stays add column reservation_id bigint references reservations (id);
alter table stays add constraint stays_reservation_id_check
    check ((credential_kind = 'reservation') = (reservation_id is not null));
-- One stay per reservation, ever (M3B-24; architecture 7 and 20 I3). Also the
-- reservation_id FK index.
create unique index stays_reservation_key on stays (reservation_id) where reservation_id is not null;

-- Reservations are kept (policies 1). slot_loads are counters and may be deleted.
revoke delete, truncate on reservations from mypark_app;
