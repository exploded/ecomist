-- Runs ------------------------------------------------------------------

-- name: ListRunsByFranchise :many
SELECT r.*,
    (SELECT COUNT(*) FROM customers c WHERE c.run_id = r.id AND c.active = 1) AS stop_count,
    (SELECT CAST(COALESCE(SUM(d.quantity), 0) AS INTEGER) FROM dispensers d
     JOIN customers c ON c.id = d.customer_id
     WHERE c.run_id = r.id AND c.active = 1 AND d.active = 1) AS unit_count
FROM runs r
WHERE r.franchise_id = ? AND r.active = 1
ORDER BY r.name;

-- name: GetRun :one
SELECT * FROM runs WHERE id = ?;

-- name: CreateRun :exec
INSERT INTO runs (franchise_id, name) VALUES (?, ?);

-- name: GetLastRun :one
SELECT * FROM runs WHERE id = (SELECT MAX(id) FROM runs);

-- name: UpdateRun :exec
UPDATE runs SET name = ?, notes = ? WHERE id = ?;

-- name: DeactivateRun :exec
UPDATE runs SET active = 0 WHERE id = ?;

-- name: ListRunCustomers :many
SELECT c.*,
    (SELECT CAST(COALESCE(SUM(d.quantity), 0) AS INTEGER) FROM dispensers d
     WHERE d.customer_id = c.id AND d.active = 1) AS unit_count
FROM customers c
WHERE c.run_id = ? AND c.active = 1
ORDER BY c.sort_order, c.id;

-- name: ListUnassignedCustomers :many
SELECT * FROM customers
WHERE franchise_id = ? AND active = 1 AND run_id IS NULL
ORDER BY name;

-- name: ListActiveCustomersByFranchise :many
SELECT * FROM customers
WHERE franchise_id = ? AND active = 1
ORDER BY name;
