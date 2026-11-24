-- name: CreateUser :exec
INSERT INTO users (id, name, email, time_zone, status, role, created_at, updated_at)
VALUES (@id, @name, @email, @time_zone, @status, @role, @created_at, @updated_at);

-- name: GetUser :one
SELECT id, name, email, time_zone, status, role, created_at, updated_at
FROM users
WHERE id = @id;

-- name: ListUsers :many
SELECT id, name, email, time_zone, status, role, created_at, updated_at
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

-- name: ListContactMethodsOfUsers :many
SELECT id, user_id, type, address, created_at, updated_at
FROM contact_methods
WHERE user_id = ANY(@user_ids::uuid[])
ORDER BY user_id, id;

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

-- name: ActiveUsers :many
SELECT id
FROM users
WHERE id = ANY(@ids::uuid[]) AND status = 'active';

-- name: GetUserByEmail :one
SELECT id, name, email, time_zone, status, role, created_at, updated_at
FROM users
WHERE email = @email;

-- name: AdminExists :one
SELECT EXISTS (SELECT 1 FROM users WHERE role = 'admin' AND status = 'active');

-- name: SetUserRole :execrows
UPDATE users
SET role = @role, updated_at = @updated_at
WHERE id = @id;

-- name: CreateIdentity :exec
INSERT INTO identities (id, user_id, issuer, subject, created_at, updated_at)
VALUES (@id, @user_id, @issuer, @subject, @created_at, @updated_at);

-- name: GetIdentity :one
SELECT id, user_id, issuer, subject, created_at, updated_at
FROM identities
WHERE issuer = @issuer AND subject = @subject;

-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at, updated_at)
VALUES (@id, @user_id, @token_hash, @expires_at, @created_at, @updated_at);

-- name: GetActiveSession :one
SELECT sessions.id, sessions.user_id, sessions.token_hash, sessions.expires_at, sessions.revoked_at, sessions.created_at, sessions.updated_at
FROM sessions
JOIN users ON users.id = sessions.user_id
WHERE sessions.token_hash = @token_hash AND sessions.revoked_at IS NULL AND sessions.expires_at > @now AND users.status = 'active';

-- name: RevokeSession :execrows
UPDATE sessions
SET revoked_at = @revoked_at, updated_at = @updated_at
WHERE token_hash = @token_hash AND revoked_at IS NULL;

-- name: CreateSignIn :exec
INSERT INTO sign_ins (id, state_hash, nonce, verifier, return_to, code_challenge, expires_at, created_at, updated_at)
VALUES (@id, @state_hash, @nonce, @verifier, sqlc.narg(return_to), sqlc.narg(code_challenge), @expires_at, @created_at, @updated_at);

-- name: UseSignIn :execrows
UPDATE sign_ins
SET used_at = @used_at, updated_at = @updated_at
WHERE state_hash = @state_hash AND used_at IS NULL AND expires_at > @now;

-- name: GetSignIn :one
SELECT id, state_hash, nonce, verifier, return_to, code_challenge, expires_at, used_at, created_at, updated_at
FROM sign_ins
WHERE state_hash = @state_hash;

-- name: DeleteExpiredSignIns :execrows
DELETE FROM sign_ins
WHERE expires_at < @before;

-- name: CreateOneTimeCode :exec
INSERT INTO one_time_codes (id, code_hash, user_id, purpose, code_challenge, expires_at, created_at, updated_at)
VALUES (@id, @code_hash, @user_id, @purpose, sqlc.narg(code_challenge), @expires_at, @created_at, @updated_at);

-- name: UseOneTimeCode :execrows
UPDATE one_time_codes
SET used_at = @used_at, updated_at = @updated_at
WHERE code_hash = @code_hash AND used_at IS NULL AND expires_at > @now;

-- name: GetOneTimeCode :one
SELECT id, code_hash, user_id, purpose, code_challenge, expires_at, used_at, created_at, updated_at
FROM one_time_codes
WHERE code_hash = @code_hash;
