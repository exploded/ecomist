-- One-time backfill: seq_label is now system-owned. Renumber every active
-- dispenser as a single running count per customer, in display order
-- (zones by sort_order, then within-zone sort_order, then id). A row's
-- quantity consumes that many numbers, so a qty-2 row reads "2 - 3" and the
-- next row starts at 4. The text format matches renumberDispensers() in Go so
-- the first in-app edit after this migration writes nothing new.
WITH ordered AS (
    SELECT d.id AS did,
           MAX(d.quantity, 1) AS qty,
           SUM(MAX(d.quantity, 1)) OVER (
               PARTITION BY d.customer_id
               ORDER BY COALESCE(z.sort_order, 999999), d.sort_order, d.id
               ROWS UNBOUNDED PRECEDING
           ) AS cum_end
    FROM dispensers d
    LEFT JOIN zones z ON z.id = d.zone_id
    WHERE d.active = 1
)
UPDATE dispensers
SET seq_label = (
    SELECT CASE
        WHEN o.qty <= 1 THEN CAST(o.cum_end AS TEXT)
        ELSE CAST(o.cum_end - o.qty + 1 AS TEXT) || ' - ' || CAST(o.cum_end AS TEXT)
    END
    FROM ordered o
    WHERE o.did = dispensers.id
)
WHERE id IN (SELECT did FROM ordered);
