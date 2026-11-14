-- Device registry and append-only device event log (design M2 section 5.2,
-- M2C-19, M2C-24, M2C-43). Forward-only: no Down section.
--
-- Runs as mypark_migrator. The DML grants to mypark_app come from the default
-- privileges set in 00001_baseline.sql. device_events then has update, delete
-- and truncate revoked: it is evidence. The only way to delete from it is
-- purge_device_events, whose 12-month floor lives inside the function.

-- +goose Up
create table devices (
    id                bigint generated always as identity primary key,
    -- Fleet devices carry fixed ids (M2C-25); others take a uuidv7().
    public_id         uuid not null unique default uuidv7(),
    lot_id            bigint not null references lots (id),
    kind              text not null check (kind in
                        ('podium', 'dispenser', 'entry_barrier', 'exit_barrier', 'pay_station', 'sensor_gateway')),
    name              text not null check (length(name) between 1 and 60),
    -- nonce (24 B) || secretbox output of the 32-byte secret (M2C-24).
    secret_ciphertext bytea not null,
    secret_key_id     text not null,
    status            text not null default 'active' check (status in ('active', 'revoked')),
    last_seen_at      timestamptz,
    revoked_at        timestamptz,
    created_at        timestamptz not null default now(),
    check ((status = 'revoked') = (revoked_at is not null)),
    -- Also the lot_id FK index.
    unique (lot_id, name)
);

create table device_events (
    id          bigint generated always as identity primary key,
    lot_id      bigint not null references lots (id),
    device_id   bigint not null references devices (id),
    event_id    uuid not null,
    type        text not null check (type in (
                  'podium.vehicle_detected', 'entry.credential_presented', 'barrier.passed',
                  'paystation.scan', 'paystation.lost_ticket', 'paystation.cash_paid',
                  'exit.credential_presented', 'sensor.space_changed', 'heartbeat')),
    -- The device's claim, informational only (M2C-13).
    occurred_at timestamptz not null,
    received_at timestamptz not null default now(),
    body_sha256 bytea not null check (octet_length(body_sha256) = 32),
    -- Credential values are redacted before they reach these columns (M2C-26).
    payload     jsonb not null,
    response    jsonb not null,
    -- Idempotency key (M2C-19); also the device_id FK index.
    unique (device_id, event_id)
);

-- The lot_id FK index, and the per-lot timeline.
create index device_events_lot_id_received_at_idx on device_events (lot_id, received_at);
-- Cheap range scan for the retention purge.
create index device_events_received_at_brin on device_events using brin (received_at);

revoke update, delete, truncate on device_events from mypark_app;

-- Retention (policies 1.8): the app role has no DELETE, so this security
-- definer function (owned by the migrator, which owns the table) is the only
-- way to purge. The 12-month floor is inside it, so the app cannot erase
-- recent evidence. Returns the number of rows deleted; callers loop until it
-- returns less than batch.
-- +goose StatementBegin
create function purge_device_events(batch int) returns bigint
language sql security definer set search_path = '' as $$
    with doomed as (
        select id from public.device_events
         where received_at < now() - interval '12 months'
         order by received_at
         limit least(greatest(batch, 1), 10000)
    ), gone as (
        delete from public.device_events d using doomed where d.id = doomed.id returning 1
    )
    select count(*) from gone
$$;
-- +goose StatementEnd

revoke all on function purge_device_events(int) from public;
grant execute on function purge_device_events(int) to mypark_app;
