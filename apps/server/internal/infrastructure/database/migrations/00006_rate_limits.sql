-- Fixed-window rate limit counters shared by every API instance (spec 5.9,
-- OC-21). Forward-only: no Down section.
--
-- Runs as mypark_migrator. The DML grants to mypark_app come from the
-- default privileges set in 00001_baseline.sql.
--
-- UNLOGGED: no WAL, so it is fast, and it is truncated after a crash. That is
-- acceptable for throttling. Keys are SHA-256 hashes, so no emails or IPs are
-- stored in clear.

-- +goose Up
create unlogged table rate_limit_counters (
    key          bytea primary key,
    window_start timestamptz not null,
    count        int not null,
    expires_at   timestamptz not null
);

-- The worker purge deletes by expiry.
create index rate_limit_counters_expires_at_idx on rate_limit_counters (expires_at);
