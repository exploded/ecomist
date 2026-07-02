-- Users -----------------------------------------------------------------

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ?;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: CreateUser :exec
INSERT INTO users (email, name, password_hash) VALUES (?, ?, ?);

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = ? WHERE id = ?;

-- name: MarkEmailVerified :exec
UPDATE users SET email_verified = 1 WHERE id = ?;

-- name: ApproveUser :exec
UPDATE users SET approved = 1, franchise_id = ? WHERE id = ?;

-- name: MakeUserAdmin :exec
UPDATE users SET is_admin = 1, approved = 1, franchise_id = NULL WHERE id = ?;

-- name: ListPendingUsers :many
SELECT * FROM users WHERE approved = 0 ORDER BY created_at DESC;

-- name: ListUsers :many
SELECT u.*, f.name AS franchise_name
FROM users u
LEFT JOIN franchises f ON f.id = u.franchise_id
ORDER BY u.approved DESC, u.name;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = ?;

-- Email verification tokens ----------------------------------------------

-- name: CreateEmailToken :exec
INSERT INTO email_tokens (token, user_id, purpose, expires_at) VALUES (?, ?, ?, ?);

-- name: GetEmailToken :one
SELECT * FROM email_tokens WHERE token = ? AND expires_at > datetime('now');

-- name: DeleteEmailTokensForUser :exec
DELETE FROM email_tokens WHERE user_id = ?;

-- Sessions --------------------------------------------------------------

-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, franchise_id, expires_at) VALUES (?, ?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions WHERE id = ? AND expires_at > datetime('now');

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= datetime('now');

-- name: SetSessionFranchise :exec
UPDATE sessions SET franchise_id = ? WHERE id = ?;

-- Approved emails -------------------------------------------------------

-- name: GetApprovedEmail :one
SELECT * FROM approved_emails WHERE email = ?;

-- name: ListApprovedEmails :many
SELECT a.email, a.franchise_id, f.name AS franchise_name
FROM approved_emails a
JOIN franchises f ON f.id = a.franchise_id
ORDER BY a.email;

-- name: CreateApprovedEmail :exec
INSERT INTO approved_emails (email, franchise_id) VALUES (?, ?)
ON CONFLICT(email) DO UPDATE SET franchise_id = excluded.franchise_id;

-- name: DeleteApprovedEmail :exec
DELETE FROM approved_emails WHERE email = ?;

-- Franchises ------------------------------------------------------------

-- name: GetFranchise :one
SELECT * FROM franchises WHERE id = ?;

-- name: GetFranchiseByName :one
SELECT * FROM franchises WHERE name = ?;

-- name: ListFranchises :many
SELECT * FROM franchises WHERE active = 1 ORDER BY name;

-- name: CreateFranchise :exec
INSERT INTO franchises (name) VALUES (?);

-- name: GetLastFranchise :one
SELECT * FROM franchises WHERE id = (SELECT MAX(id) FROM franchises);

-- name: UpdateFranchiseName :exec
UPDATE franchises SET name = ? WHERE id = ?;
