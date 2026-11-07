-- Identity schema for M1: users, login sessions, refresh tokens, one-time
-- codes and the legal documents users accept. Forward-only: no Down section.
--
-- Runs as mypark_migrator. The DML grants to mypark_app come from the
-- default privileges set in 00001_baseline.sql.

-- +goose Up
create table users (
    id                   bigint generated always as identity primary key,
    public_id            uuid not null default uuidv7() unique,
    email                citext not null check (length(email) <= 254),
    -- PHC argon2id string. Null for invited admins before acceptance and for
    -- deleted users.
    password_hash        text,
    password_changed_at  timestamptz,
    first_name           text not null check (length(first_name) between 1 and 60),
    -- Holds both surnames (es-DO convention).
    last_name            text not null check (length(last_name) between 1 and 80),
    phone                text check (phone ~ '^\+[1-9][0-9]{6,14}$'),
    role                 text not null default 'driver' check (role in ('driver', 'admin')),
    status               text not null default 'pending_verification'
                         check (status in ('pending_verification', 'active', 'suspended', 'deleted')),
    locale               text not null default 'es-DO' check (locale in ('es-DO', 'en')),
    email_verified_at    timestamptz,
    terms_version        text,
    terms_accepted_at    timestamptz,
    privacy_version      text,
    privacy_accepted_at  timestamptz,
    created_at           timestamptz not null default now(),
    updated_at           timestamptz not null default now(),
    deleted_at           timestamptz,
    check ((terms_version is null) = (terms_accepted_at is null)),
    check ((privacy_version is null) = (privacy_accepted_at is null)),
    -- Keeps the two deletion markers consistent.
    check ((status = 'deleted') = (deleted_at is not null))
);

create unique index users_email_active_key on users (email) where deleted_at is null;
create index users_admin_idx on users (role) where role = 'admin' and deleted_at is null;

-- One row per login, which is one refresh-token family.
create table auth_sessions (
    id                  bigint generated always as identity primary key,
    -- The JWT sid claim.
    public_id           uuid not null default uuidv7() unique,
    user_id             bigint not null references users (id),
    client_kind         text not null check (client_kind in ('web', 'mobile')),
    created_at          timestamptz not null default now(),
    last_used_at        timestamptz not null default now(),
    absolute_expires_at timestamptz not null,
    revoked_at          timestamptz,
    revoked_reason      text check (revoked_reason in
                        ('logout', 'logout_all', 'password_reset', 'reuse_detected', 'account_deleted')),
    created_ip          inet,
    user_agent          text,
    check ((revoked_at is null) = (revoked_reason is null))
);

-- Full FK index, plus the hot subset of live sessions.
create index auth_sessions_user_id_idx on auth_sessions (user_id);
create index auth_sessions_user_id_live_idx on auth_sessions (user_id) where revoked_at is null;

create table refresh_tokens (
    id         bigint generated always as identity primary key,
    session_id bigint not null references auth_sessions (id),
    -- SHA-256 of a 256-bit random token: high entropy, so no pepper.
    token_hash bytea not null unique,
    created_at timestamptz not null default now(),
    expires_at timestamptz not null,
    -- Set when the token is used. Non-null on presentation means reuse.
    rotated_at timestamptz
);

create index refresh_tokens_session_id_idx on refresh_tokens (session_id);
create index refresh_tokens_expires_at_idx on refresh_tokens (expires_at);

create table auth_codes (
    id          bigint generated always as identity primary key,
    user_id     bigint not null references users (id),
    purpose     text not null check (purpose in ('verify_email', 'reset_password', 'admin_invite')),
    -- HMAC-SHA256(pepper, purpose || user public_id || code).
    code_hash   bytea not null,
    expires_at  timestamptz not null,
    attempts    smallint not null default 0 check (attempts between 0 and 5),
    -- Set on success or when a newer code replaces this one.
    consumed_at timestamptz,
    created_at  timestamptz not null default now()
);

create index auth_codes_user_id_idx on auth_codes (user_id);
-- At most one live code per user and purpose.
create unique index auth_codes_one_live_key on auth_codes (user_id, purpose) where consumed_at is null;
create index auth_codes_expires_at_idx on auth_codes (expires_at);

create table legal_documents (
    id           bigint generated always as identity primary key,
    kind         text not null check (kind in ('terms', 'privacy')),
    version      text not null,
    published_at timestamptz not null,
    url_es       text not null,
    url_en       text not null,
    unique (kind, version)
);

create index legal_documents_kind_published_idx on legal_documents (kind, published_at desc);
