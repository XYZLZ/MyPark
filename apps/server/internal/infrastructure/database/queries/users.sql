-- name: GetUserProfileByID :one
select id, public_id, email, first_name, last_name, phone, role, status, locale,
       email_verified_at, terms_version, terms_accepted_at, privacy_version,
       privacy_accepted_at, created_at
from users
where id = @id and deleted_at is null;

-- name: UpdateUserLegalAcceptance :exec
update users
set terms_version = @terms_version,
    terms_accepted_at = @now,
    privacy_version = @privacy_version,
    privacy_accepted_at = @now,
    updated_at = @now
where id = @id and deleted_at is null;

-- name: UpdateUserProfile :one
-- Partial profile update. A null narg leaves the column as it is; phone is
-- written only when @set_phone is true, so a null phone clears it.
update users
set first_name = coalesce(sqlc.narg('first_name'), first_name),
    last_name = coalesce(sqlc.narg('last_name'), last_name),
    locale = coalesce(sqlc.narg('locale'), locale),
    phone = case when @set_phone::boolean then sqlc.narg('phone') else phone end,
    updated_at = @now
where id = @id and deleted_at is null
returning id, public_id, email, first_name, last_name, phone, role, status, locale,
          email_verified_at, terms_version, terms_accepted_at, privacy_version,
          privacy_accepted_at, created_at;

-- name: AnonymizeUserProfile :execrows
-- The profile half of account deletion (spec 7.8). The auth-owned columns
-- (email, password, status, deleted_at) follow in EraseAuthUser, in the same
-- transaction.
update users
set first_name = 'deleted',
    last_name = 'deleted',
    phone = null,
    updated_at = @now
where id = @id and deleted_at is null;

-- name: ListAdmins :many
-- Keyset page of admin users, newest first. A null cursor_at starts at the
-- top; otherwise rows strictly after (cursor_at, cursor_id) in the order.
select id, public_id, email, first_name, last_name, status, locale, created_at
from users
where role = 'admin' and deleted_at is null
  and (sqlc.narg('cursor_at')::timestamptz is null
       or (created_at, id) < (sqlc.narg('cursor_at')::timestamptz, sqlc.narg('cursor_id')::bigint))
order by created_at desc, id desc
limit @page_size;
