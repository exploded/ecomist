-- Dispenser models --------------------------------------------------------

-- name: ListModels :many
SELECT * FROM dispenser_models ORDER BY active DESC, name;

-- name: SearchModels :many
SELECT * FROM dispenser_models
WHERE active = 1 AND name LIKE ?
ORDER BY name
LIMIT 10;

-- name: GetModel :one
SELECT * FROM dispenser_models WHERE id = ?;

-- name: GetModelByName :one
SELECT * FROM dispenser_models WHERE name = ?;

-- name: CreateModel :exec
INSERT INTO dispenser_models (name) VALUES (?);

-- name: UpdateModel :exec
UPDATE dispenser_models SET name = ?, active = ? WHERE id = ?;

-- Fragrances ---------------------------------------------------------------

-- name: ListFragrances :many
SELECT * FROM fragrances ORDER BY active DESC, name;

-- name: SearchFragrances :many
SELECT * FROM fragrances
WHERE active = 1 AND name LIKE ?
ORDER BY name
LIMIT 10;

-- name: GetFragrance :one
SELECT * FROM fragrances WHERE id = ?;

-- name: GetFragranceByName :one
SELECT * FROM fragrances WHERE name = ?;

-- name: CreateFragrance :exec
INSERT INTO fragrances (name, default_size_ml) VALUES (?, ?);

-- name: UpdateFragrance :exec
UPDATE fragrances SET name = ?, default_size_ml = ?, active = ? WHERE id = ?;
