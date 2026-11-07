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
