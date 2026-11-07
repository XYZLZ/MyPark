-- name: HitRateLimit :one
-- Fixed-window counter (spec 10.2). The window start comes from @now (the
-- injected clock) through date_bin, never from the database clock. A row left
-- over from an older window restarts at 1.
insert into rate_limit_counters as c (key, window_start, count, expires_at)
values (
    @key,
    date_bin(@window_size::interval, @now::timestamptz, 'epoch'::timestamptz),
    1,
    date_bin(@window_size::interval, @now::timestamptz, 'epoch'::timestamptz) + @window_size::interval
)
on conflict (key) do update set
    count        = case when c.window_start = excluded.window_start then c.count + 1 else 1 end,
    window_start = excluded.window_start,
    expires_at   = excluded.expires_at
returning count, expires_at;

-- name: PurgeRateLimits :execrows
delete from rate_limit_counters where expires_at < @now;
