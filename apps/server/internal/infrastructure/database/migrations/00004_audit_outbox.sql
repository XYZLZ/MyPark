-- Append-only audit log and the transactional email outbox. Forward-only: no
-- Down section.
--
-- Runs as mypark_migrator. The DML grants to mypark_app come from the default
-- privileges set in 00001_baseline.sql; audit_log then has them revoked.

-- +goose Up
create table audit_log (
    id            bigint generated always as identity primary key,
    occurred_at   timestamptz not null default now(),
    actor_kind    text not null check (actor_kind in ('user', 'admin', 'system', 'anonymous')),
    actor_user_id bigint references users (id),
    action        text not null,
    -- Public ids only.
    entity_type   text,
    entity_id     text,
    -- Non-PII metadata only: ids, versions and changed field names.
    before        jsonb,
    after         jsonb,
    ip            inet,
    request_id    text
);

create index audit_log_occurred_at_idx on audit_log (occurred_at desc);
create index audit_log_actor_user_id_idx on audit_log (actor_user_id);
create index audit_log_entity_idx on audit_log (entity_type, entity_id);

-- Append-only for the application role (default privileges granted these).
revoke update, delete, truncate on audit_log from mypark_app;

create table outbox_messages (
    id           bigint generated always as identity primary key,
    kind         text not null default 'email' check (kind in ('email')),
    template     text not null check (template in
                     ('verify_email', 'password_reset', 'password_changed', 'account_exists', 'admin_invite')),
    locale       text not null check (locale in ('es-DO', 'en')),
    to_address   citext not null,
    user_id      bigint references users (id),
    -- Template data. "code" is removed once the message is sent or dead.
    payload      jsonb not null,
    dedupe_key   text not null unique,
    status       text not null default 'pending' check (status in ('pending', 'sending', 'sent', 'dead')),
    attempts     smallint not null default 0,
    -- Truncated by the worker; never holds the payload.
    last_error   text,
    available_at timestamptz not null default now(),
    created_at   timestamptz not null default now(),
    sent_at      timestamptz
);

create index outbox_messages_user_id_idx on outbox_messages (user_id);
-- Worker claim query.
create index outbox_messages_claim_idx on outbox_messages (available_at) where status in ('pending', 'sending');
