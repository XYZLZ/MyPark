-- name: HitRateLimit :one
-- Fixed-window counter (spec 10.2). The window start comes from @now (the
-- injected clock) through date_bin, never from the database clock. A row left
-- over from an older window restarts at 1. A hit stamped with an older window
-- than the stored one (an instance whose clock lags) counts in the stored
-- window instead of resetting it, so the window never moves backwards.
insert into rate_limit_counters as c (key, window_start, count, expires_at)
values (
    @key,
    date_bin(@window_size::interval, @now::timestamptz, 'epoch'::timestamptz),
    1,
    date_bin(@window_size::interval, @now::timestamptz, 'epoch'::timestamptz) + @window_size::interval
)
on conflict (key) do update set
    count        = case when excluded.window_start > c.window_start then 1 else c.count + 1 end,
    window_start = greatest(c.window_start, excluded.window_start),
    expires_at   = greatest(c.expires_at, excluded.expires_at)
returning count, expires_at;

-- name: PurgeRateLimits :execrows
delete from rate_limit_counters where expires_at < @now;
