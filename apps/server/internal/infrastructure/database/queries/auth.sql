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
