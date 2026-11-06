-- name: UpsertLotBySlug :one
-- Inserts the lot or updates its descriptive fields, and always returns its
-- id (do nothing would return no row on conflict). updated_at changes only
-- when a value actually changed. latitude and longitude are not touched.
insert into lots (slug, name, address, capacity)
values (@slug, @name, @address, @capacity)
on conflict (slug) do update
set name = excluded.name,
    address = excluded.address,
    capacity = excluded.capacity,
    updated_at = case
        when (lots.name, lots.address, lots.capacity)
            is distinct from (excluded.name, excluded.address, excluded.capacity)
        then now()
        else lots.updated_at
    end
returning id;

-- name: InsertSpace :exec
insert into spaces (lot_id, code)
values (@lot_id, @code)
on conflict (lot_id, code) do nothing;

-- name: CountSpacesByLot :one
select count(*) from spaces
where lot_id = @lot_id;

-- name: InitLotOccupancy :exec
insert into lot_occupancy (lot_id)
values (@lot_id)
on conflict do nothing;
