-- name: EnqueueOutboxEmail :exec
-- Idempotent on dedupe_key.
insert into outbox_messages (kind, template, locale, to_address, user_id, payload, dedupe_key)
values ('email', @template, @locale, @to_address, sqlc.narg('user_id'), @payload, @dedupe_key)
on conflict (dedupe_key) do nothing;

-- name: ScrubPendingOutboxCodesForUser :exec
update outbox_messages
set payload = payload - 'code'
where user_id = @user_id
  and status in ('pending', 'sending')
  and payload ? 'code';
