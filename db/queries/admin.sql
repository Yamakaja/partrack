-- Admin queries: instance-level user management.

-- name: ListAllUsers :many
SELECT u.id, u.display_name, u.email, u.is_active, u.is_admin, u.created_at,
       (uc.id IS NOT NULL) AS has_password
FROM users u
LEFT JOIN user_credentials uc ON uc.user_id = u.id
ORDER BY u.created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountAllUsers :one
SELECT COUNT(*) FROM users;

-- name: GetUserForAdmin :one
SELECT u.id, u.display_name, u.email, u.is_active, u.is_admin, u.created_at,
       (uc.id IS NOT NULL) AS has_password
FROM users u
LEFT JOIN user_credentials uc ON uc.user_id = u.id
WHERE u.id = $1;

-- name: AdminCreateUser :one
INSERT INTO users (display_name, email)
VALUES ($1, $2)
RETURNING id, display_name, email, is_active, is_admin, created_at;

-- name: AdminUpdateUser :one
UPDATE users
SET display_name = COALESCE(sqlc.narg('display_name'), display_name),
    email        = COALESCE(sqlc.narg('email'), email),
    is_active    = COALESCE(sqlc.narg('is_active'), is_active),
    is_admin     = COALESCE(sqlc.narg('is_admin'), is_admin)
WHERE id = sqlc.arg('id')
RETURNING id, display_name, email, is_active, is_admin, created_at;

-- name: AdminSetPassword :exec
INSERT INTO user_credentials (user_id, password_hash)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE
SET password_hash = EXCLUDED.password_hash, updated_at = NOW();

-- name: CreatePasswordResetToken :one
INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING id, user_id, expires_at, created_at;

-- name: GetPasswordResetToken :one
SELECT t.id, t.user_id, t.expires_at, t.used_at,
       u.display_name, u.email
FROM password_reset_tokens t
JOIN users u ON u.id = t.user_id
WHERE t.token_hash = $1;

-- name: UsePasswordResetToken :exec
UPDATE password_reset_tokens SET used_at = NOW() WHERE id = $1;

-- name: DeleteSessionsByUserID :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: DeleteTokensByUserID :exec
DELETE FROM api_tokens WHERE user_id = $1;
