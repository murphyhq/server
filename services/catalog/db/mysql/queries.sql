-- name: CreateService :exec
INSERT INTO services (id, name, description, team_id, escalation_policy_id, urgency, status, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(description), sqlc.arg(team_id), sqlc.arg(escalation_policy_id), sqlc.arg(urgency), sqlc.arg(status), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: GetService :one
SELECT id, name, description, team_id, escalation_policy_id, urgency, status, created_at, updated_at
FROM services
WHERE id = sqlc.arg(id);

-- name: ListServices :many
SELECT id, name, description, team_id, escalation_policy_id, urgency, status, created_at, updated_at
FROM services
WHERE id < sqlc.arg(before)
ORDER BY id DESC
LIMIT ?;

-- name: UpdateService :execrows
UPDATE services
SET name = sqlc.arg(name), description = sqlc.arg(description), team_id = sqlc.arg(team_id), escalation_policy_id = sqlc.arg(escalation_policy_id),
    urgency = sqlc.arg(urgency), status = sqlc.arg(status), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: DeleteService :execrows
DELETE FROM services
WHERE id = sqlc.arg(id);

-- name: CreateIntegration :exec
INSERT INTO integrations (id, service_id, name, type, key_version, previous_key_valid_until, status, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(service_id), sqlc.arg(name), sqlc.arg(type), sqlc.arg(key_version), sqlc.narg(previous_key_valid_until), sqlc.arg(status), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: GetIntegration :one
SELECT id, service_id, name, type, key_version, previous_key_valid_until, status, created_at, updated_at
FROM integrations
WHERE id = sqlc.arg(id);

-- name: ListIntegrations :many
SELECT id, service_id, name, type, key_version, previous_key_valid_until, status, created_at, updated_at
FROM integrations
WHERE service_id = sqlc.arg(service_id) AND id < sqlc.arg(before)
ORDER BY id DESC
LIMIT ?;

-- name: UpdateIntegration :execrows
UPDATE integrations
SET name = sqlc.arg(name), status = sqlc.arg(status), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: DeleteIntegration :execrows
DELETE FROM integrations
WHERE id = sqlc.arg(id);

-- name: RotateIntegrationKey :execrows
UPDATE integrations
SET key_version = key_version + 1, previous_key_valid_until = sqlc.arg(previous_key_valid_until), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);
