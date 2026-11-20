-- Reservation-fee payments, refunds, the simulated gateway ledger and
-- reconciliation alerts (design M3 section 6.2, M2C-41, M3B-7 to M3B-11,
-- M3B-27 to M3B-30). Forward-only: no Down section.
--
-- Runs as mypark_migrator. The DML grants to mypark_app come from the default
-- privileges set in 00001_baseline.sql.

-- +goose Up
alter table payments alter column stay_id drop not null;
alter table payments
    add column reservation_id      bigint references reservations (id),
    -- M3B-27.
    add column user_id             bigint references users (id),
    -- Idempotency-Key (M3B-7).
    add column client_key          text check (client_key ~ '^[A-Za-z0-9_-]{8,64}$'),
    add column request_fingerprint bytea check (octet_length(request_fingerprint) = 32),
    add column provider_ref        text check (length(provider_ref) between 1 and 100),
    add column card_brand          text check (card_brand in ('visa', 'mastercard', 'amex', 'other')),
    add column card_last4          text check (card_last4 ~ '^[0-9]{4}$'),
    add column failure_code        text check (failure_code in ('card_declined', 'card_not_supported', 'not_submitted')),
    add column failed_at           timestamptz,
    add column refunded_at         timestamptz,
    add constraint payments_target_check      check (num_nonnulls(reservation_id, stay_id) = 1),
    add constraint payments_kind_target_check check ((kind = 'reservation_fee') = (reservation_id is not null)),
    add constraint payments_gateway_check     check ((method = 'gateway_sim') = (kind = 'reservation_fee')),
    add constraint payments_fee_fields_check  check (kind <> 'reservation_fee' or (user_id is not null
        and client_key is not null and request_fingerprint is not null
        and card_brand is not null and card_last4 is not null)),
    add constraint payments_failed_check      check ((status = 'failed') = (failed_at is not null and failure_code is not null)),
    add constraint payments_refunded_check    check ((status = 'refunded') = (refunded_at is not null)),
    add constraint payments_provider_check    check (status not in ('succeeded', 'refunded') or kind <> 'reservation_fee'
        or provider_ref is not null);

-- No double charge (M3B-8).
create unique index payments_reservation_live_key on payments (reservation_id)
    where kind = 'reservation_fee' and status in ('pending', 'succeeded', 'refunded');
-- Idempotency-Key scope (M3B-7); also the reservation_id FK index.
create unique index payments_reservation_client_key on payments (reservation_id, client_key)
    where reservation_id is not null;
-- Receipts keyset (M3B-37); also the user_id FK index.
create index payments_user_created_idx on payments (user_id, created_at desc, id desc) where user_id is not null;
-- Reconcile job (M3B-10).
create index payments_pending_fee_idx on payments (created_at) where kind = 'reservation_fee' and status = 'pending';

create table refunds (
    id              bigint generated always as identity primary key,
    public_id       uuid not null unique default uuidv7(),
    payment_id      bigint not null references payments (id),
    -- Always the full fee in M3.
    amount_cents    bigint not null check (amount_cents > 0),
    reason          text not null check (reason in ('cancelled_before_arrival', 'facility_unavailable',
                                                    'facility_closed', 'admin_override')),
    status          text not null default 'pending' check (status in ('pending', 'succeeded', 'failed')),
    -- 'refund:<public_id>', the gateway refund key.
    idempotency_key text not null unique,
    attempts        smallint not null default 0 check (attempts >= 0),
    -- Lease and backoff (M3B-29). The service writes the injected now; no default.
    next_attempt_at timestamptz not null,
    -- Never holds card data.
    last_error      text check (length(last_error) <= 500),
    provider_ref    text,
    created_at      timestamptz not null default now(),
    succeeded_at    timestamptz,
    failed_at       timestamptz,
    constraint refunds_succeeded_check check ((status = 'succeeded') = (succeeded_at is not null)),
    constraint refunds_failed_check check ((status = 'failed') = (failed_at is not null)),
    constraint refunds_provider_check check (status <> 'succeeded' or provider_ref is not null)
);

-- No double refund.
create unique index refunds_payment_live_key on refunds (payment_id) where status in ('pending', 'succeeded');
create index refunds_due_idx on refunds (next_attempt_at) where status = 'pending';
create index refunds_payment_id_idx on refunds (payment_id);

-- The simulated gateway's own ledger (architecture 9), owned by
-- infrastructure/paymentsim.
create table sim_gateway_charges (
    id                     bigint generated always as identity primary key,
    -- 'rsv-fee:<payment public_id>'.
    idempotency_key        text not null unique,
    amount_cents           bigint not null check (amount_cents > 0),
    currency               text not null default 'DOP' check (currency = 'DOP'),
    outcome                text not null check (outcome in ('succeeded', 'declined')),
    decline_code           text check (decline_code in ('card_declined', 'card_not_supported')),
    card_brand             text not null check (card_brand in ('visa', 'mastercard', 'amex', 'other')),
    card_last4             text not null check (card_last4 ~ '^[0-9]{4}$'),
    -- 'sim_ch_' plus 24 random base32 characters.
    provider_ref           text not null unique,
    -- The timeout card (M3B-11).
    delayed                boolean not null default false,
    created_at             timestamptz not null default now(),
    -- Full refunds only, at most one per charge.
    refund_idempotency_key text unique,
    refund_provider_ref    text unique,
    refunded_at            timestamptz,
    constraint sim_gateway_charges_decline_check check ((outcome = 'declined') = (decline_code is not null)),
    constraint sim_gateway_charges_refund_check check ((refund_idempotency_key is null) = (refunded_at is null))
);

-- Minimal for M3 (M3B-30); M4 swaps the kind constraint.
create table reconciliation_alerts (
    id              bigint generated always as identity primary key,
    public_id       uuid not null unique default uuidv7(),
    lot_id          bigint not null references lots (id),
    kind            text not null check (kind in ('reservation_unadmitted', 'refund_failed')),
    -- Public ids only: {reservation_id, refund_id?, payment_id?}.
    details         jsonb not null,
    opened_at       timestamptz not null,
    -- M4 writes these.
    resolved_at     timestamptz,
    resolved_by     bigint references users (id),
    resolution_note text
);

-- FK index, and the open alerts per lot.
create index reconciliation_alerts_lot_id_idx on reconciliation_alerts (lot_id);
create index reconciliation_alerts_open_idx on reconciliation_alerts (lot_id) where resolved_at is null;
create index reconciliation_alerts_resolved_by_idx on reconciliation_alerts (resolved_by) where resolved_by is not null;

-- Financial and reconciliation records are kept (policies 1). payments
-- already has them revoked since 00009.
revoke delete, truncate on refunds, sim_gateway_charges, reconciliation_alerts from mypark_app;

-- The generated name of the inline column check is outbox_messages_template_check.
alter table outbox_messages drop constraint outbox_messages_template_check;
alter table outbox_messages add constraint outbox_messages_template_check check (template in
    ('verify_email', 'password_reset', 'password_changed', 'account_exists', 'admin_invite',
     'reservation_confirmed', 'reservation_reminder', 'reservation_cancelled', 'reservation_no_show',
     'refund_succeeded'));
