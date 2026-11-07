-- name: InsertUser :one
insert into users (email, password_hash, password_changed_at, first_name, last_name, phone,
                   locale, terms_version, terms_accepted_at, privacy_version, privacy_accepted_at)
values (@email, @password_hash, @password_changed_at, @first_name, @last_name, @phone,
        @locale, @terms_version, @terms_accepted_at, @privacy_version, @privacy_accepted_at)
returning id, public_id;

-- name: GetActiveUserByEmail :one
-- Credential columns of the non-deleted account with this email (citext, so
-- the match ignores case).
select id, public_id, email, password_hash, role, status, email_verified_at, locale
from users
where email = @email and deleted_at is null;

-- name: GetUserCredentialsByID :one
select id, public_id, email, password_hash, role, status, email_verified_at, locale
from users
where id = @id;

-- name: ConsumeLiveAuthCodes :exec
update auth_codes
set consumed_at = @now
where user_id = @user_id and purpose = @purpose and consumed_at is null;

-- name: InsertAuthCode :one
insert into auth_codes (user_id, purpose, code_hash, expires_at, created_at)
values (@user_id, @purpose, @code_hash, @expires_at, @now)
returning id;

-- name: InsertAuthSession :one
insert into auth_sessions (user_id, client_kind, created_at, last_used_at, absolute_expires_at,
                           created_ip, user_agent)
values (@user_id, @client_kind, @now, @now, @absolute_expires_at, @created_ip, @user_agent)
returning id, public_id;

-- name: InsertRefreshToken :exec
insert into refresh_tokens (session_id, token_hash, created_at, expires_at)
values (@session_id, @token_hash, @now, @expires_at);

-- name: RevokeAuthSession :exec
update auth_sessions
set revoked_at = @now, revoked_reason = @reason
where id = @id and revoked_at is null;
