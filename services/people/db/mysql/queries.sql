-- name: CreateUser :exec
INSERT INTO users (id, name, email, time_zone, status, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(email), sqlc.arg(time_zone), sqlc.arg(status), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: GetUser :one
SELECT id, name, email, time_zone, status, created_at, updated_at
FROM users
WHERE id = sqlc.arg(id);

-- name: ListUsers :many
SELECT id, name, email, time_zone, status, created_at, updated_at
FROM users
WHERE id < sqlc.arg(before)
ORDER BY id DESC
LIMIT ?;

-- name: UpdateUser :execrows
UPDATE users
SET name = sqlc.arg(name), email = sqlc.arg(email), time_zone = sqlc.arg(time_zone), status = sqlc.arg(status), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: DeleteUser :execrows
DELETE FROM users
WHERE id = sqlc.arg(id);

-- name: CreateContactMethod :exec
INSERT INTO contact_methods (id, user_id, type, address, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(user_id), sqlc.arg(type), sqlc.arg(address), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: GetContactMethod :one
SELECT id, user_id, type, address, created_at, updated_at
FROM contact_methods
WHERE id = sqlc.arg(id);

-- name: ListContactMethods :many
SELECT id, user_id, type, address, created_at, updated_at
FROM contact_methods
WHERE user_id = sqlc.arg(user_id)
ORDER BY id;

-- name: UpdateContactMethod :execrows
UPDATE contact_methods
SET type = sqlc.arg(type), address = sqlc.arg(address), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: DeleteContactMethod :execrows
DELETE FROM contact_methods
WHERE id = sqlc.arg(id);

-- name: CreateTeam :exec
INSERT INTO teams (id, name, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: GetTeam :one
SELECT id, name, created_at, updated_at
FROM teams
WHERE id = sqlc.arg(id);

-- name: ListTeams :many
SELECT id, name, created_at, updated_at
FROM teams
WHERE id < sqlc.arg(before)
ORDER BY id DESC
LIMIT ?;

-- name: UpdateTeam :execrows
UPDATE teams
SET name = sqlc.arg(name), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: DeleteTeam :execrows
DELETE FROM teams
WHERE id = sqlc.arg(id);

-- name: SetMembership :exec
INSERT INTO team_memberships (id, team_id, user_id, role, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(team_id), sqlc.arg(user_id), sqlc.arg(role), sqlc.arg(created_at), sqlc.arg(updated_at))
ON DUPLICATE KEY UPDATE role = VALUES(role), updated_at = VALUES(updated_at);

-- name: GetMembership :one
SELECT id, team_id, user_id, role, created_at, updated_at
FROM team_memberships
WHERE team_id = sqlc.arg(team_id) AND user_id = sqlc.arg(user_id);

-- name: ListMemberships :many
SELECT id, team_id, user_id, role, created_at, updated_at
FROM team_memberships
WHERE team_id = sqlc.arg(team_id)
ORDER BY id;

-- name: DeleteMembership :execrows
DELETE FROM team_memberships
WHERE team_id = sqlc.arg(team_id) AND user_id = sqlc.arg(user_id);

-- name: ExistingUsers :many
SELECT id
FROM users
WHERE id IN (sqlc.slice(ids));

-- name: ExistingTeams :many
SELECT id
FROM teams
WHERE id IN (sqlc.slice(ids));

-- name: ActiveUsers :many
SELECT id
FROM users
WHERE id IN (sqlc.slice(ids)) AND status = 'active';
