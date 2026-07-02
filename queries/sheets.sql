-- Run sheets (an execution of a run on a date) ----------------------------

-- name: CreateRunSheet :exec
INSERT INTO run_sheets (run_id, run_date, created_by) VALUES (?, ?, ?);

-- name: GetLastRunSheet :one
SELECT * FROM run_sheets WHERE id = (SELECT MAX(id) FROM run_sheets);

-- name: GetRunSheet :one
SELECT s.*, r.name AS run_name, r.franchise_id
FROM run_sheets s
JOIN runs r ON r.id = s.run_id
WHERE s.id = ?;

-- name: GetOpenRunSheetForRun :one
SELECT * FROM run_sheets WHERE run_id = ? AND status = 'open' ORDER BY id DESC LIMIT 1;

-- name: ListOpenRunSheetsByFranchise :many
SELECT s.*, r.name AS run_name,
    (SELECT COUNT(*) FROM run_sheet_stops st WHERE st.run_sheet_id = s.id) AS stop_count,
    (SELECT COUNT(*) FROM run_sheet_stops st
     WHERE st.run_sheet_id = s.id AND st.status != 'pending') AS done_count
FROM run_sheets s
JOIN runs r ON r.id = s.run_id
WHERE r.franchise_id = ? AND s.status = 'open'
ORDER BY s.created_at DESC;

-- name: ListRecentCompletedSheets :many
SELECT s.*, r.name AS run_name,
    (SELECT COUNT(*) FROM run_sheet_stops st WHERE st.run_sheet_id = s.id) AS stop_count
FROM run_sheets s
JOIN runs r ON r.id = s.run_id
WHERE r.franchise_id = ? AND s.status = 'completed'
ORDER BY s.completed_at DESC
LIMIT 10;

-- name: CompleteRunSheet :exec
UPDATE run_sheets SET status = 'completed', completed_at = datetime('now') WHERE id = ?;

-- name: ReopenRunSheet :exec
UPDATE run_sheets SET status = 'open', completed_at = NULL WHERE id = ?;

-- name: SaveRunSheetSignature :exec
UPDATE run_sheets SET signature = ?, signed_by = ?, signed_at = datetime('now') WHERE id = ?;

-- name: ClearRunSheetSignature :exec
UPDATE run_sheets SET signature = '', signed_by = '', signed_at = NULL WHERE id = ?;

-- Stops -------------------------------------------------------------------

-- name: CreateStopsForSheet :exec
INSERT INTO run_sheet_stops (run_sheet_id, customer_id, sort_order)
SELECT ?, id, sort_order FROM customers WHERE run_id = ? AND active = 1;

-- name: ListStops :many
SELECT st.*, c.name AS customer_name, c.address_line, c.suburb, c.phone,
    c.map_ref, c.access_notes, c.service_minutes,
    (SELECT CAST(COALESCE(SUM(d.quantity), 0) AS INTEGER) FROM dispensers d
     WHERE d.customer_id = st.customer_id AND d.active = 1) AS total_units,
    (SELECT CAST(COALESCE(SUM(d.quantity), 0) AS INTEGER) FROM dispensers d
     JOIN run_sheet_ticks t ON t.dispenser_id = d.id AND t.run_sheet_id = st.run_sheet_id
     WHERE d.customer_id = st.customer_id AND d.active = 1) AS ticked_units
FROM run_sheet_stops st
JOIN customers c ON c.id = st.customer_id
WHERE st.run_sheet_id = ?
ORDER BY st.sort_order, st.id;

-- name: GetStop :one
SELECT st.*, c.name AS customer_name, c.address_line, c.suburb, c.phone,
    c.map_ref, c.access_notes, c.general_notes, c.service_minutes,
    (SELECT CAST(COALESCE(SUM(d.quantity), 0) AS INTEGER) FROM dispensers d
     WHERE d.customer_id = st.customer_id AND d.active = 1) AS total_units,
    (SELECT CAST(COALESCE(SUM(d.quantity), 0) AS INTEGER) FROM dispensers d
     JOIN run_sheet_ticks t ON t.dispenser_id = d.id AND t.run_sheet_id = st.run_sheet_id
     WHERE d.customer_id = st.customer_id AND d.active = 1) AS ticked_units
FROM run_sheet_stops st
JOIN customers c ON c.id = st.customer_id
WHERE st.id = ?;

-- name: GetStopBySheetAndCustomer :one
SELECT * FROM run_sheet_stops WHERE run_sheet_id = ? AND customer_id = ?;

-- name: UpdateStopStatus :exec
UPDATE run_sheet_stops
SET status = ?, note = ?, completed_at = datetime('now'), completed_by = ?
WHERE id = ?;

-- name: ReopenStop :exec
UPDATE run_sheet_stops
SET status = 'pending', completed_at = NULL, completed_by = NULL
WHERE id = ?;

-- name: UpdateStopNote :exec
UPDATE run_sheet_stops SET note = ? WHERE id = ?;

-- name: SetStopSortOrder :exec
UPDATE run_sheet_stops SET sort_order = ? WHERE id = ?;

-- name: CountPendingStops :one
SELECT COUNT(*) FROM run_sheet_stops WHERE run_sheet_id = ? AND status = 'pending';

-- Ticks -------------------------------------------------------------------

-- name: CreateTick :exec
INSERT INTO run_sheet_ticks (run_sheet_id, dispenser_id, ticked_by)
VALUES (?, ?, ?)
ON CONFLICT(run_sheet_id, dispenser_id) DO NOTHING;

-- name: DeleteTick :exec
DELETE FROM run_sheet_ticks WHERE run_sheet_id = ? AND dispenser_id = ?;

-- name: UpdateTickNote :exec
UPDATE run_sheet_ticks SET note = ? WHERE run_sheet_id = ? AND dispenser_id = ?;

-- name: GetTick :one
SELECT * FROM run_sheet_ticks WHERE run_sheet_id = ? AND dispenser_id = ?;

-- Dispensers with tick state for a stop ------------------------------------

-- name: ListStopDispensers :many
SELECT d.*, m.name AS model_name, f.name AS fragrance_name,
    z.label AS zone_label, z.area AS zone_area, z.access_notes AS zone_access_notes,
    z.sort_order AS zone_sort,
    t.id AS tick_id, t.ticked_at, t.note AS tick_note
FROM dispensers d
JOIN dispenser_models m ON m.id = d.model_id
LEFT JOIN fragrances f ON f.id = d.fragrance_id
LEFT JOIN zones z ON z.id = d.zone_id
LEFT JOIN run_sheet_ticks t ON t.dispenser_id = d.id AND t.run_sheet_id = ?
WHERE d.customer_id = ? AND d.active = 1
ORDER BY COALESCE(z.sort_order, 999999), d.sort_order, d.id;

-- name: GetStopDispenser :one
SELECT d.*, m.name AS model_name, f.name AS fragrance_name,
    z.label AS zone_label, z.area AS zone_area, z.access_notes AS zone_access_notes,
    z.sort_order AS zone_sort,
    t.id AS tick_id, t.ticked_at, t.note AS tick_note
FROM dispensers d
JOIN dispenser_models m ON m.id = d.model_id
LEFT JOIN fragrances f ON f.id = d.fragrance_id
LEFT JOIN zones z ON z.id = d.zone_id
LEFT JOIN run_sheet_ticks t ON t.dispenser_id = d.id AND t.run_sheet_id = ?
WHERE d.id = ?;
