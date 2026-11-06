-- Baseline schema for M0: the parking lot, its spaces and the occupancy
-- counter. Forward-only: there is no Down section.
--
-- Runs as mypark_migrator (owner of the database and the public schema).
-- The roles themselves are created outside migrations, by
-- deploy/compose/postgres/init/01-roles.sh.

-- +goose Up
create extension if not exists citext;
create extension if not exists btree_gist;

create table lots (
    id            bigint generated always as identity primary key,
    public_id     uuid not null default uuidv7() unique,
    slug          text not null unique,
    name          text not null,
    address       text not null,
    latitude      numeric(9, 6),
    longitude     numeric(9, 6),
    timezone      text not null default 'America/Santo_Domingo',
    capacity      int not null check (capacity > 0),
    status        text not null default 'open' check (status in ('open', 'closed')),
    opening_hours jsonb not null default '{"always_open": true}',
    created_at    timestamptz not null default now(),
    updated_at    timestamptz not null default now()
);

create table spaces (
    id                bigint generated always as identity primary key,
    lot_id            bigint not null references lots (id),
    code              text not null,
    kind              text not null default 'standard' check (kind in ('standard', 'accessible')),
    status            text not null default 'free' check (status in ('free', 'occupied', 'out_of_service')),
    sensor_updated_at timestamptz,
    -- Also serves as the index for the lot_id foreign key (leading column).
    unique (lot_id, code)
);

create index spaces_lot_id_status_idx on spaces (lot_id, status);

create table lot_occupancy (
    lot_id     bigint primary key references lots (id),
    occupied   int not null default 0 check (occupied >= 0),
    version    bigint not null default 0,
    updated_at timestamptz not null default now()
);

-- Privileges: mypark_app gets DML only, never DDL.
revoke all on schema public from public;
grant usage on schema public to mypark_app;

-- Objects created later by mypark_migrator get the same DML grants.
alter default privileges for role mypark_migrator in schema public
    grant select, insert, update, delete on tables to mypark_app;
alter default privileges for role mypark_migrator in schema public
    grant usage, select on sequences to mypark_app;

-- Explicit grants on the baseline objects, so they hold whichever role ran
-- this migration (the defaults above apply only to mypark_migrator).
grant select, insert, update, delete on lots, spaces, lot_occupancy to mypark_app;
grant usage, select on all sequences in schema public to mypark_app;

-- goose creates its version table before this migration runs, so the
-- default privileges above never cover it. The app role reads it for
-- /readyz and must not write it.
grant select on goose_db_version to mypark_app;
