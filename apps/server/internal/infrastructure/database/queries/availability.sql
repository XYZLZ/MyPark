-- name: GetLotSnapshot :one
-- One read for the availability snapshot (design §11.2): the lot, its
-- occupancy counter and the out-of-service count. Pending entries are not
-- counted here; the entry decision counts them under the lot lock.
select l.public_id, l.capacity, l.status,
       o.occupied, o.version, o.updated_at,
       (select count(*) from spaces s where s.lot_id = l.id and s.status = 'out_of_service')::int as out_of_service
from lots l
join lot_occupancy o on o.lot_id = l.id
where l.id = @lot_id;

-- name: ListSpaceStates :many
-- Code and state only: the public payload has no device or stay data (M2C-7).
select code, status from spaces
where lot_id = @lot_id
order by code;
