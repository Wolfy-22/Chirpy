-- name: CreateToken :one
insert into refresh_tokens (token, created_at, updated_at, user_id, expires_at)
values (
    $1, 
    now(),
    now(),
    $2, 
    $3
)
returning *;

-- name: GetUserFromRefreshToken :one
SELECT users.* FROM users
JOIN refresh_tokens ON users.id = refresh_tokens.user_id
WHERE refresh_tokens.token = $1
AND revoked_at IS NULL
AND expires_at > NOW();

-- name: RevokeRefreshToken :one
UPDATE refresh_tokens SET revoked_at = NOW(),
updated_at = NOW()
WHERE token = $1
RETURNING *;