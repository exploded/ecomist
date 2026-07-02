-- Customers -----------------------------------------------------------------

-- name: ListCustomersByFranchise :many
SELECT c.*, r.name AS run_name,
    (SELECT CAST(COALESCE(SUM(d.quantity), 0) AS INTEGER) FROM dispensers d
     WHERE d.customer_id = c.id AND d.active = 1) AS unit_count
FROM customers c
LEFT JOIN runs r ON r.id = c.run_id
WHERE c.franchise_id = ? AND c.active = 1
  AND (? = '' OR c.name LIKE ? OR c.suburb LIKE ?)
ORDER BY c.name;

-- name: GetCustomer :one
SELECT * FROM customers WHERE id = ?;

-- name: CreateCustomer :exec
INSERT INTO customers (franchise_id, run_id, sort_order, name)
VALUES (?, ?, (SELECT COALESCE(MAX(c2.sort_order), 0) + 1 FROM customers c2 WHERE c2.run_id = ?), ?);

-- name: GetLastCustomer :one
SELECT * FROM customers WHERE id = (SELECT MAX(id) FROM customers);

-- name: UpdateCustomer :exec
UPDATE customers SET
    name = ?, address_line = ?, suburb = ?, phone = ?, map_ref = ?,
    regarding = ?, service_minutes = ?, access_notes = ?, general_notes = ?
WHERE id = ?;

-- name: DeactivateCustomer :exec
UPDATE customers SET active = 0, run_id = NULL WHERE id = ?;

-- name: AssignCustomerToRun :exec
UPDATE customers SET run_id = ?,
    sort_order = (SELECT COALESCE(MAX(c2.sort_order), 0) + 1 FROM customers c2 WHERE c2.run_id = ?)
WHERE customers.id = ?;

-- name: RemoveCustomerFromRun :exec
UPDATE customers SET run_id = NULL WHERE id = ?;

-- name: SetCustomerSortOrder :exec
UPDATE customers SET sort_order = ? WHERE id = ?;

-- name: CustomerModelTally :many
SELECT m.name AS model_name, CAST(SUM(d.quantity) AS INTEGER) AS units
FROM dispensers d
JOIN dispenser_models m ON m.id = d.model_id
WHERE d.customer_id = ? AND d.active = 1
GROUP BY m.id
ORDER BY units DESC, m.name;

-- Contacts --------------------------------------------------------------

-- name: ListContactsByCustomer :many
SELECT * FROM contacts WHERE customer_id = ? ORDER BY is_primary DESC, id;

-- name: GetContact :one
SELECT * FROM contacts WHERE id = ?;

-- name: CreateContact :exec
INSERT INTO contacts (customer_id, name, role, is_primary, phone) VALUES (?, ?, ?, ?, ?);

-- name: GetLastContact :one
SELECT * FROM contacts WHERE id = (SELECT MAX(id) FROM contacts);

-- name: UpdateContact :exec
UPDATE contacts SET name = ?, role = ?, is_primary = ?, phone = ?, notes = ? WHERE id = ?;

-- name: DeleteContact :exec
DELETE FROM contacts WHERE id = ?;

-- Zones -----------------------------------------------------------------

-- name: ListZonesByCustomer :many
SELECT * FROM zones WHERE customer_id = ? ORDER BY sort_order, id;

-- name: GetZone :one
SELECT * FROM zones WHERE id = ?;

-- name: GetZoneByLabel :one
SELECT * FROM zones WHERE customer_id = ? AND label = ? COLLATE NOCASE LIMIT 1;

-- name: CreateZone :exec
INSERT INTO zones (customer_id, sort_order, label, area)
VALUES (?, (SELECT COALESCE(MAX(z2.sort_order), 0) + 1 FROM zones z2 WHERE z2.customer_id = ?), ?, ?);

-- name: GetLastZone :one
SELECT * FROM zones WHERE id = (SELECT MAX(id) FROM zones);

-- name: UpdateZone :exec
UPDATE zones SET label = ?, area = ?, access_notes = ?, notes = ? WHERE id = ?;

-- name: DeleteZone :exec
DELETE FROM zones WHERE id = ?;

-- Dispensers ------------------------------------------------------------

-- name: ListDispensersByCustomer :many
SELECT d.*, m.name AS model_name, f.name AS fragrance_name,
    z.label AS zone_label, z.area AS zone_area, z.sort_order AS zone_sort
FROM dispensers d
JOIN dispenser_models m ON m.id = d.model_id
LEFT JOIN fragrances f ON f.id = d.fragrance_id
LEFT JOIN zones z ON z.id = d.zone_id
WHERE d.customer_id = ? AND d.active = 1
ORDER BY COALESCE(z.sort_order, 999999), d.sort_order, d.id;

-- name: GetDispenser :one
SELECT d.*, m.name AS model_name, f.name AS fragrance_name
FROM dispensers d
JOIN dispenser_models m ON m.id = d.model_id
LEFT JOIN fragrances f ON f.id = d.fragrance_id
WHERE d.id = ?;

-- name: CreateDispenser :exec
INSERT INTO dispensers (customer_id, zone_id, sort_order, seq_label, location,
    model_id, quantity, fragrance_id, fragrance_note, refill_size_ml,
    service_interval_days, notes)
VALUES (?, ?, (SELECT COALESCE(MAX(d2.sort_order), 0) + 1 FROM dispensers d2 WHERE d2.customer_id = ?),
    ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetLastDispenser :one
SELECT d.*, m.name AS model_name, f.name AS fragrance_name
FROM dispensers d
JOIN dispenser_models m ON m.id = d.model_id
LEFT JOIN fragrances f ON f.id = d.fragrance_id
WHERE d.id = (SELECT MAX(id) FROM dispensers);

-- name: UpdateDispenser :exec
UPDATE dispensers SET
    zone_id = ?, seq_label = ?, location = ?, model_id = ?, quantity = ?,
    fragrance_id = ?, fragrance_note = ?, refill_size_ml = ?,
    service_interval_days = ?, notes = ?
WHERE id = ?;

-- name: DeactivateDispenser :exec
UPDATE dispensers SET active = 0 WHERE id = ?;
