-- Employee cards, stays and the minimal payments table (design M2 sections
-- 5.3 and 5.4, M2C-3, M2C-28, M2C-41). Forward-only: no Down section.
--
-- Runs as mypark_migrator. The DML grants to mypark_app come from the default
-- privileges set in 00001_baseline.sql.

-- +goose Up
create table employee_cards (
    id            bigint generated always as identity primary key,
    public_id     uuid not null unique default uuidv7(),
    lot_id        bigint not null references lots (id),
    -- HMAC-SHA256 with CARD_UID_PEPPER over the normalized UID (M2C-26). The
    -- UID itself is never stored.
    card_uid_hash bytea not null unique check (octet_length(card_uid_hash) = 32),
    holder_name   text not null check (length(holder_name) between 1 and 80),
    employee_code text not null check (length(employee_code) between 1 and 20),
    status        text not null default 'active' check (status in ('active', 'revoked')),
    created_by    bigint references users (id),
    created_at    timestamptz not null default now(),
    revoked_at    timestamptz,
    check ((status = 'revoked') = (revoked_at is not null))
);

-- FK indexes.
create index employee_cards_lot_id_idx on employee_cards (lot_id);
create index employee_cards_created_by_idx on employee_cards (created_by) where created_by is not null;

create table stays (
    id                 bigint generated always as identity primary key,
    -- The stay_id devices see.
    public_id          uuid not null unique default uuidv7(),
    lot_id             bigint not null references lots (id),
    -- 'reservation' is unused until M3.
    credential_kind    text not null check (credential_kind in ('ticket', 'reservation', 'employee_card')),
    ticket_code_hash   bytea unique,
    employee_card_id   bigint references employee_cards (id),
    status             text not null check (status in ('entry_pending', 'active', 'paid', 'exited', 'voided')),
    -- Captured at acceptance (M2C-2).
    tariff_version_id  bigint not null references tariff_versions (id),
    accepted_at        timestamptz not null default now(),
    entered_at         timestamptz,
    paid_at            timestamptz,
    exit_deadline_at   timestamptz,
    exit_authorized_at timestamptz,
    exited_at          timestamptz,
    voided_at          timestamptz,
    lost_ticket        boolean not null default false,
    updated_at         timestamptz not null default now(),
    check ((credential_kind = 'ticket') = (ticket_code_hash is not null)),
    check ((credential_kind = 'employee_card') = (employee_card_id is not null)),
    check (status not in ('active', 'paid', 'exited') or entered_at is not null),
    check (status <> 'paid' or (paid_at is not null and exit_deadline_at is not null)),
    -- The fee calculator reads the payment time next to the deadline: a zero
    -- paid_at would bill from the year 1 (about RD$42.7M).
    check (exit_deadline_at is null or paid_at is not null),
    check (status <> 'exited' or exited_at is not null),
    check (status <> 'voided' or voided_at is not null),
    check (not lost_ticket or credential_kind = 'ticket')
);

-- FK indexes. The partial ones serve the hot lookups; stays_lot_id_idx covers
-- the lot_id FK for every status.
create index stays_lot_id_idx on stays (lot_id);
create index stays_open_idx on stays (lot_id) where status in ('entry_pending', 'active', 'paid');
-- Decision count (M2C-30) and the void job.
create index stays_pending_idx on stays (lot_id, accepted_at) where status = 'entry_pending';
-- Anti-passback (M2C-28): one open stay per card.
create unique index stays_card_open_key on stays (employee_card_id)
    where status in ('entry_pending', 'active', 'paid');
create index stays_employee_card_id_idx on stays (employee_card_id) where employee_card_id is not null;
create index stays_tariff_version_id_idx on stays (tariff_version_id);

create table payments (
    id              bigint generated always as identity primary key,
    -- receipt_no = 'MP-' || lpad(id::text, 8, '0'), derived (M2C-34).
    public_id       uuid not null unique default uuidv7(),
    kind            text not null check (kind in ('reservation_fee', 'parking_fee')),
    -- M3 drops not null and adds reservation_id.
    stay_id         bigint not null references stays (id),
    method          text not null check (method in ('gateway_sim', 'cash', 'none')),
    amount_cents    bigint not null check (amount_cents >= 0),
    status          text not null check (status in ('pending', 'succeeded', 'failed', 'refunded')),
    -- 'cash:<device public_id>:<event_id>' or 'zero:<stay public_id>'.
    idempotency_key text not null unique,
    -- The pay station.
    device_id       bigint references devices (id),
    created_at      timestamptz not null default now(),
    succeeded_at    timestamptz,
    check ((method = 'none') = (amount_cents = 0))
);

create index payments_stay_id_idx on payments (stay_id);
create index payments_device_id_idx on payments (device_id) where device_id is not null;
-- At most one zero payment per stay.
create unique index payments_zero_per_stay_key on payments (stay_id) where method = 'none';
