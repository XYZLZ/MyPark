-- name: InsertAuditLog :exec
insert into audit_log (actor_kind, actor_user_id, action, entity_type, entity_id, before, after, ip, request_id)
values (@actor_kind, sqlc.narg('actor_user_id'), @action, sqlc.narg('entity_type'), sqlc.narg('entity_id'),
        sqlc.narg('before'), sqlc.narg('after'), sqlc.narg('ip'), sqlc.narg('request_id'));
