-- Vehicles owned by drivers. Forward-only: no Down section.
--
-- Runs as mypark_migrator. The DML grants to mypark_app come from the
-- default privileges set in 00001_baseline.sql.

-- +goose Up
create table vehicles (
    id               bigint generated always as identity primary key,
    public_id        uuid not null default uuidv7() unique,
    user_id          bigint not null references users (id),
    -- Canonical form used for uniqueness: upper case, no spaces or dashes.
    plate_normalized text not null check (plate_normalized ~ '^[A-Z0-9]{2,10}$'),
    -- The plate as the driver typed it (trimmed).
    plate_display    text not null check (length(plate_display) between 2 and 15),
    make             text not null check (length(make) between 1 and 50),
    model            text not null check (length(model) between 1 and 50),
    color            text not null check (length(color) between 1 and 30),
    created_at       timestamptz not null default now(),
    updated_at       timestamptz not null default now(),
    deleted_at       timestamptz
);

-- A plate is unique system-wide among active vehicles (FR-VEH-3); deleting a
-- vehicle frees it.
create unique index vehicles_plate_active_key on vehicles (plate_normalized) where deleted_at is null;
-- Full FK index, plus the owner's list.
create index vehicles_user_id_idx on vehicles (user_id);
create index vehicles_user_active_idx on vehicles (user_id, created_at) where deleted_at is null;
