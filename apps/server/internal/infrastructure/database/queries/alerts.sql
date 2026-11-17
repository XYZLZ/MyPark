-- name: InsertReconciliationAlert :exec
-- An open incident for an admin (M3B-30). details carries public ids only. The
-- opened_at comes from the injected clock.
insert into reconciliation_alerts (lot_id, kind, details, opened_at)
values (@lot_id, @kind, @details, @opened_at);
