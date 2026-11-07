-- name: LockUserByEmail :one
-- The non-deleted account with this email, row-locked so concurrent
-- registrations of one address serialize.
select id, public_id, role, status, locale
from users
where email = @email and deleted_at is null
for update;

-- name: LockPendingDriverByEmail :one
-- Only a driver that has not verified yet. A pending admin invitee, an active
-- account and an unknown address all return no row (OC-38).
select id, public_id, locale
from users
where email = @email and deleted_at is null
  and role = 'driver' and status = 'pending_verification'
for update;

-- name: InsertPendingDriver :one
insert into users (email, password_hash, password_changed_at, first_name, last_name, phone, locale,
                   role, status, terms_version, terms_accepted_at, privacy_version,
                   privacy_accepted_at, created_at, updated_at)
values (@email, @password_hash, @now, @first_name, @last_name, sqlc.narg('phone'), @locale,
        'driver', 'pending_verification', @terms_version, @now, @privacy_version, @now, @now, @now)
returning id, public_id;

-- name: OverwritePendingDriver :exec
-- Replaces the profile and credentials of a never-verified driver (OC-12).
update users
set password_hash = @password_hash,
    password_changed_at = @now,
    first_name = @first_name,
    last_name = @last_name,
    phone = sqlc.narg('phone'),
    locale = @locale,
    terms_version = @terms_version,
    terms_accepted_at = @now,
    privacy_version = @privacy_version,
    privacy_accepted_at = @now,
    updated_at = @now
where id = @id and role = 'driver' and status = 'pending_verification' and deleted_at is null;

-- name: ActivateUser :exec
update users
set status = 'active', email_verified_at = @now, updated_at = @now
where id = @id and status = 'pending_verification';

-- name: ConsumeLiveAuthCodes :exec
update auth_codes
set consumed_at = @now
where user_id = @user_id and purpose = @purpose and consumed_at is null;

-- name: InsertAuthCode :one
insert into auth_codes (user_id, purpose, code_hash, expires_at, created_at)
values (@user_id, @purpose, @code_hash, @expires_at, @now)
returning id;

-- name: AttemptAuthCode :one
-- Counts one attempt on the live code and returns it. Runs first and commits
-- even when the code is wrong, so attempts survive the failing request.
update auth_codes
set attempts = attempts + 1
where user_id = @user_id and purpose = @purpose and consumed_at is null
  and expires_at > @now and attempts < @max_attempts::smallint
returning id, code_hash, attempts;

-- name: ConsumeAuthCode :exec
update auth_codes set consumed_at = @now where id = @id and consumed_at is null;

-- name: FindUserForLogin :one
-- The non-deleted account with this email, unlocked: login verifies the
-- password before it opens a transaction.
select id, public_id, email, password_hash, first_name, last_name, phone, role, status, locale,
       email_verified_at, terms_version, terms_accepted_at, privacy_version,
       privacy_accepted_at, created_at
from users
where email = @email and deleted_at is null;

-- name: RehashPassword :exec
-- Compare-and-set, so a password changed since the login read is never
-- overwritten by the upgrade.
update users
set password_hash = @new_hash, updated_at = @now
where id = @id and password_hash = @old_hash;

-- name: SetPassword :exec
-- Password reset: stores the new hash and stamps the change.
update users
set password_hash = @password_hash, password_changed_at = @now, updated_at = @now
where id = @id and status = 'active' and deleted_at is null;

-- name: InsertAuthSession :one
insert into auth_sessions (user_id, client_kind, created_at, last_used_at, absolute_expires_at,
                           created_ip, user_agent)
values (@user_id, @client_kind, @now, @now, @absolute_expires_at, sqlc.narg('created_ip'),
        sqlc.narg('user_agent'))
returning id, public_id;

-- name: InsertRefreshToken :exec
insert into refresh_tokens (session_id, token_hash, created_at, expires_at)
values (@session_id, @token_hash, @now, @expires_at);

-- name: GetSessionPrincipal :one
-- The per-request session check (spec 7.3): the session and its user in one
-- indexed lookup. Both public ids must match, so a token that names another
-- user's session finds nothing. Expiry is judged against @now, not now().
select s.id as session_id, s.revoked_at, (s.absolute_expires_at <= @now) as expired,
       u.id as user_id, u.role, u.status, u.terms_version, u.privacy_version
from auth_sessions s
join users u on u.id = s.user_id
where s.public_id = @session_public_id and u.public_id = @user_public_id;

-- name: RevokeSession :execrows
-- Idempotent: an already revoked session keeps its original reason and time.
update auth_sessions
set revoked_at = @now, revoked_reason = @reason
where id = @id and revoked_at is null;

-- name: RevokeUserSessions :execrows
-- Revokes every live session of a user and keeps the reason of the ones that
-- were already revoked.
update auth_sessions
set revoked_at = @now, revoked_reason = @reason
where user_id = @user_id and revoked_at is null;

-- name: LockRefreshToken :one
-- The presented token with its session and user. The token and session rows
-- are locked, so two requests with one token serialize and the second sees
-- rotated_at (spec 7.4). The user is read, not locked.
select t.id as token_id, t.rotated_at, t.expires_at as token_expires_at,
       s.id as session_id, s.public_id as session_public_id, s.revoked_at,
       s.absolute_expires_at,
       u.id as user_id, u.public_id as user_public_id, u.email, u.first_name, u.last_name,
       u.phone, u.role, u.status, u.locale, u.email_verified_at, u.terms_version,
       u.terms_accepted_at, u.privacy_version, u.privacy_accepted_at, u.created_at as user_created_at
from refresh_tokens t
join auth_sessions s on s.id = t.session_id
join users u on u.id = s.user_id
where t.token_hash = @token_hash
for update of t, s;

-- name: RotateRefreshToken :exec
update refresh_tokens set rotated_at = @now where id = @id and rotated_at is null;

-- name: TouchAuthSession :exec
update auth_sessions set last_used_at = @now where id = @id;

-- name: PurgeAuthCodes :execrows
delete from auth_codes where expires_at < @cutoff;

-- name: PurgeRefreshTokens :execrows
-- Tokens past their expiry, and every token of a session being purged.
delete from refresh_tokens
where expires_at < @cutoff
   or session_id in (
       select s.id from auth_sessions s
       where s.revoked_at < @cutoff or s.absolute_expires_at < @cutoff
   );

-- name: PurgeAuthSessions :execrows
-- Sessions revoked or expired before @cutoff.
delete from auth_sessions
where revoked_at < @cutoff or absolute_expires_at < @cutoff;
