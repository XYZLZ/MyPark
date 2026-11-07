-- name: CurrentLegalDocuments :many
-- The latest document per kind published at or before @now.
select distinct on (kind) kind, version, published_at, url_es, url_en
from legal_documents
where published_at <= @now
order by kind, published_at desc, id desc;

-- name: InsertLegalDocument :exec
insert into legal_documents (kind, version, published_at, url_es, url_en)
values (@kind, @version, @published_at, @url_es, @url_en)
on conflict (kind, version) do nothing;
