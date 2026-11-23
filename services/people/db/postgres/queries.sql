-- name: CreateUser :exec
INSERT INTO users (id, name, email, time_zone, status, created_at, updated_at)
VALUES (@id, @name, @email, @time_zone, @status, @created_at, @updated_at);

-- name: GetUser :one
SELECT id, name, email, time_zone, status, created_at, updated_at
FROM users
WHERE id = @id;

-- name: ListUsers :many
SELECT id, name, email, time_zone, status, created_at, updated_at
FROM users
WHERE id < @before
ORDER BY id DESC
LIMIT @max_rows;

-- name: UpdateUser :execrows
UPDATE users
SET name = @name, email = @email, time_zone = @time_zone, status = @status, updated_at = @updated_at
WHERE id = @id;

-- name: DeleteUser :execrows
DELETE FROM users
WHERE id = @id;

-- name: CreateContactMethod :exec
INSERT INTO contact_methods (id, user_id, type, address, created_at, updated_at)
VALUES (@id, @user_id, @type, @address, @created_at, @updated_at);

-- name: GetContactMethod :one
SELECT id, user_id, type, address, created_at, updated_at
FROM contact_methods
WHERE id = @id;

-- name: ListContactMethods :many
SELECT id, user_id, type, address, created_at, updated_at
FROM contact_methods
WHERE user_id = @user_id
ORDER BY id;

-- name: UpdateContactMethod :execrows
UPDATE contact_methods
SET type = @type, address = @address, updated_at = @updated_at
WHERE id = @id;

-- name: DeleteContactMethod :execrows
DELETE FROM contact_methods
WHERE id = @id;

-- name: CreateTeam :exec
INSERT INTO teams (id, name, created_at, updated_at)
VALUES (@id, @name, @created_at, @updated_at);

-- name: GetTeam :one
SELECT id, name, created_at, updated_at
FROM teams
WHERE id = @id;

-- name: ListTeams :many
SELECT id, name, created_at, updated_at
FROM teams
WHERE id < @before
ORDER BY id DESC
LIMIT @max_rows;

-- name: UpdateTeam :execrows
UPDATE teams
SET name = @name, updated_at = @updated_at
WHERE id = @id;

-- name: DeleteTeam :execrows
DELETE FROM teams
WHERE id = @id;

-- name: SetMembership :exec
INSERT INTO team_memberships (id, team_id, user_id, role, created_at, updated_at)
VALUES (@id, @team_id, @user_id, @role, @created_at, @updated_at)
ON CONFLICT ON CONSTRAINT team_memberships_team_id_user_id_key
DO UPDATE SET role = EXCLUDED.role, updated_at = EXCLUDED.updated_at;

-- name: GetMembership :one
SELECT id, team_id, user_id, role, created_at, updated_at
FROM team_memberships
WHERE team_id = @team_id AND user_id = @user_id;

-- name: ListMemberships :many
SELECT id, team_id, user_id, role, created_at, updated_at
FROM team_memberships
WHERE team_id = @team_id
ORDER BY id;

-- name: DeleteMembership :execrows
DELETE FROM team_memberships
WHERE team_id = @team_id AND user_id = @user_id;

-- name: ExistingUsers :many
SELECT id
FROM users
WHERE id = ANY(@ids::uuid[]);

-- name: ExistingTeams :many
SELECT id
FROM teams
WHERE id = ANY(@ids::uuid[]);
