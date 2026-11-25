-- name: CreateUser :exec
INSERT INTO users (id, name, email, time_zone, status, role, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(email), sqlc.arg(time_zone), sqlc.arg(status), sqlc.arg(role), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: GetUser :one
SELECT id, name, email, time_zone, status, role, created_at, updated_at
FROM users
WHERE id = sqlc.arg(id);

-- name: ListUsers :many
SELECT id, name, email, time_zone, status, role, created_at, updated_at
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
INSERT INTO contact_methods (id, user_id, type, address, push_platform, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(user_id), sqlc.arg(type), sqlc.arg(address), sqlc.narg(push_platform), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: GetContactMethod :one
SELECT id, user_id, type, address, push_platform, created_at, updated_at, disabled_at, disabled_reason
FROM contact_methods
WHERE id = sqlc.arg(id);

-- name: ListContactMethods :many
SELECT id, user_id, type, address, push_platform, created_at, updated_at, disabled_at, disabled_reason
FROM contact_methods
WHERE user_id = sqlc.arg(user_id)
ORDER BY id;

-- name: ListContactMethodsOfUsers :many
SELECT id, user_id, type, address, push_platform, created_at, updated_at, disabled_at, disabled_reason
FROM contact_methods
WHERE user_id IN (sqlc.slice(user_ids)) AND disabled_at IS NULL
ORDER BY user_id, id;

-- name: UpdateContactMethod :execrows
UPDATE contact_methods
SET type = sqlc.arg(type), address = sqlc.arg(address), updated_at = sqlc.arg(updated_at), disabled_at = sqlc.narg(disabled_at), disabled_reason = sqlc.narg(disabled_reason)
WHERE id = sqlc.arg(id);

-- name: DisableContactMethod :exec
UPDATE contact_methods
SET disabled_at = sqlc.arg(disabled_at), disabled_reason = sqlc.arg(disabled_reason), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND address = sqlc.arg(address) AND disabled_at IS NULL;

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

-- name: UserNames :many
SELECT id, name
FROM users
WHERE id IN (sqlc.slice(ids));

-- name: TeamNames :many
SELECT id, name
FROM teams
WHERE id IN (sqlc.slice(ids));

-- name: ActiveUsers :many
SELECT id
FROM users
WHERE id IN (sqlc.slice(ids)) AND status = 'active';

-- name: GetUserByEmail :one
SELECT id, name, email, time_zone, status, role, created_at, updated_at
FROM users
WHERE email = sqlc.arg(email);

-- name: AdminExists :one
SELECT EXISTS (SELECT 1 FROM users WHERE role = 'admin' AND status = 'active');

-- name: SetUserRole :execrows
UPDATE users
SET role = sqlc.arg(role), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: CreateIdentity :exec
INSERT INTO identities (id, user_id, issuer, subject, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(user_id), sqlc.arg(issuer), sqlc.arg(subject), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: GetIdentity :one
SELECT id, user_id, issuer, subject, created_at, updated_at
FROM identities
WHERE issuer = sqlc.arg(issuer) AND subject = sqlc.arg(subject);

-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, token_hash, client, expires_at, renewable_until, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(user_id), sqlc.arg(token_hash), sqlc.arg(client), sqlc.arg(expires_at), sqlc.arg(renewable_until), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: GetActiveSession :one
SELECT sessions.id, sessions.user_id, sessions.token_hash, sessions.client, sessions.expires_at, sessions.renewable_until, sessions.revoked_at, sessions.created_at, sessions.updated_at, users.role
FROM sessions
JOIN users ON users.id = sessions.user_id
WHERE sessions.token_hash = sqlc.arg(token_hash) AND sessions.revoked_at IS NULL AND sessions.expires_at > sqlc.arg(now) AND users.status = 'active';

-- name: RenewSession :exec
UPDATE sessions
SET expires_at = sqlc.arg(expires_at), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: RevokeSession :execrows
UPDATE sessions
SET revoked_at = sqlc.arg(revoked_at), updated_at = sqlc.arg(updated_at)
WHERE token_hash = sqlc.arg(token_hash) AND revoked_at IS NULL;

-- name: CreateSignIn :exec
INSERT INTO sign_ins (id, state_hash, nonce, verifier, return_to, code_challenge, expires_at, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(state_hash), sqlc.arg(nonce), sqlc.arg(verifier), sqlc.narg(return_to), sqlc.narg(code_challenge), sqlc.arg(expires_at), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: UseSignIn :execrows
UPDATE sign_ins
SET used_at = sqlc.arg(used_at), updated_at = sqlc.arg(updated_at)
WHERE state_hash = sqlc.arg(state_hash) AND used_at IS NULL AND expires_at > sqlc.arg(now);

-- name: GetSignIn :one
SELECT id, state_hash, nonce, verifier, return_to, code_challenge, expires_at, used_at, created_at, updated_at
FROM sign_ins
WHERE state_hash = sqlc.arg(state_hash);

-- name: DeleteExpiredSignIns :execrows
DELETE FROM sign_ins
WHERE expires_at < sqlc.arg(before);

-- name: CreateOneTimeCode :exec
INSERT INTO one_time_codes (id, code_hash, user_id, purpose, code_challenge, expires_at, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(code_hash), sqlc.arg(user_id), sqlc.arg(purpose), sqlc.narg(code_challenge), sqlc.arg(expires_at), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: UseOneTimeCode :execrows
UPDATE one_time_codes
SET used_at = sqlc.arg(used_at), updated_at = sqlc.arg(updated_at)
WHERE code_hash = sqlc.arg(code_hash) AND used_at IS NULL AND expires_at > sqlc.arg(now);

-- name: GetOneTimeCode :one
SELECT id, code_hash, user_id, purpose, code_challenge, expires_at, used_at, created_at, updated_at
FROM one_time_codes
WHERE code_hash = sqlc.arg(code_hash);

-- name: ListActiveSessions :many
SELECT id, user_id, token_hash, client, expires_at, renewable_until, revoked_at, created_at, updated_at
FROM sessions
WHERE user_id = sqlc.arg(user_id) AND revoked_at IS NULL AND expires_at > sqlc.arg(now) AND id < sqlc.arg(before)
ORDER BY id DESC
LIMIT ?;

-- name: RevokeSessionOfUser :execrows
UPDATE sessions
SET revoked_at = sqlc.arg(revoked_at), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND revoked_at IS NULL;

-- name: RevokeSessionsOfUser :execrows
UPDATE sessions
SET revoked_at = sqlc.arg(revoked_at), updated_at = sqlc.arg(updated_at)
WHERE user_id = sqlc.arg(user_id) AND revoked_at IS NULL;

-- name: DiscardOneTimeCodesOfUser :execrows
UPDATE one_time_codes
SET used_at = sqlc.arg(used_at), updated_at = sqlc.arg(updated_at)
WHERE user_id = sqlc.arg(user_id) AND used_at IS NULL;

-- name: ListMembershipsOfUser :many
SELECT id, team_id, user_id, role, created_at, updated_at
FROM team_memberships
WHERE user_id = sqlc.arg(user_id)
ORDER BY team_id;

-- name: SetPassword :exec
INSERT INTO passwords (id, user_id, hash, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(user_id), sqlc.arg(hash), sqlc.arg(created_at), sqlc.arg(updated_at))
ON DUPLICATE KEY UPDATE hash = VALUES(hash), updated_at = VALUES(updated_at);

-- name: GetPasswordHash :one
SELECT hash
FROM passwords
WHERE user_id = sqlc.arg(user_id);

-- name: ListNotificationRules :many
SELECT id, user_id, urgency, position, contact_method_id, delay_seconds, created_at
FROM notification_rules
WHERE user_id = sqlc.arg(user_id)
ORDER BY urgency, position;

-- name: DeleteNotificationRules :exec
DELETE FROM notification_rules
WHERE user_id = sqlc.arg(user_id) AND urgency = sqlc.arg(urgency);
