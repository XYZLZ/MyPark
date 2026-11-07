-- name: EnqueueOutboxEmail :exec
-- Idempotent on dedupe_key. available_at comes from the injected clock.
insert into outbox_messages (kind, template, locale, to_address, user_id, payload, dedupe_key, available_at)
values ('email', @template, @locale, @to_address, sqlc.narg('user_id'), @payload, @dedupe_key, @available_at)
on conflict (dedupe_key) do nothing;

-- name: ScrubPendingOutboxCodesForUser :exec
update outbox_messages
set payload = payload - 'code'
where user_id = @user_id
  and status in ('pending', 'sending')
  and payload ? 'code';

-- name: ClaimOutboxBatch :many
-- Leases up to @batch due rows: they become 'sending' until @lease_until. A
-- row whose lease expired is claimed again (crashed worker).
update outbox_messages
set status = 'sending', attempts = attempts + 1, available_at = @lease_until
where id in (
    select o.id from outbox_messages o
    where o.status in ('pending', 'sending') and o.available_at <= @now
    order by o.available_at, o.id
    limit sqlc.arg('batch')
    for update skip locked
)
returning id, template, locale, to_address, payload, dedupe_key, attempts;

-- name: MarkOutboxSent :exec
update outbox_messages
set status = 'sent', sent_at = @now, last_error = null, payload = payload - 'code'
where id = @id;

-- name: MarkOutboxRetry :exec
update outbox_messages
set status = 'pending', available_at = @available_at, last_error = @last_error
where id = @id;

-- name: MarkOutboxDead :exec
update outbox_messages
set status = 'dead', last_error = @last_error, payload = payload - 'code'
where id = @id;

-- name: PurgeOutbox :execrows
-- sent and dead rows older than @cutoff.
delete from outbox_messages
where status in ('sent', 'dead') and created_at < @cutoff;
