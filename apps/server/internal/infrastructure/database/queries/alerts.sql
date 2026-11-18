-- name: InsertReconciliationAlert :exec
-- An open incident for an admin (M3B-30). details carries public ids only. The
-- opened_at comes from the injected clock.
insert into reconciliation_alerts (lot_id, kind, details, opened_at)
values (@lot_id, @kind, @details, @opened_at);

-- name: OpenApplyFailedAlert :execrows
-- A gateway success the reconcile job could not apply (money taken, nothing
-- recorded). Opened once per payment: a later failure of the same payment
-- inserts nothing. Two runners racing could both insert; the job runs on one
-- worker, and a duplicate alert is harmless.
insert into reconciliation_alerts (lot_id, kind, details, opened_at)
select r.lot_id, 'refund_failed',
       jsonb_build_object('reservation_id', r.public_id::text, 'payment_id', p.public_id::text, 'reason', 'apply_failed'),
       @now
from payments p
join reservations r on r.id = p.reservation_id
where p.id = @payment_id
  and not exists (select 1 from reconciliation_alerts a
                  where a.kind = 'refund_failed' and a.details->>'reason' = 'apply_failed'
                    and a.details->>'payment_id' = p.public_id::text);
