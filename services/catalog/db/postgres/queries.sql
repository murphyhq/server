-- name: CreateService :exec
INSERT INTO services (id, name, description, escalation_policy_id, urgency, status, created_at, updated_at)
VALUES (@id, @name, @description, @escalation_policy_id, @urgency, @status, @created_at, @updated_at);

-- name: GetService :one
SELECT id, name, description, escalation_policy_id, urgency, status, created_at, updated_at
FROM services
WHERE id = @id;

-- name: ListServices :many
SELECT id, name, description, escalation_policy_id, urgency, status, created_at, updated_at
FROM services
WHERE id < @before
ORDER BY id DESC
LIMIT @max_rows;

-- name: UpdateService :execrows
UPDATE services
SET name = @name, description = @description, escalation_policy_id = @escalation_policy_id,
    urgency = @urgency, status = @status, updated_at = @updated_at
WHERE id = @id;

-- name: DeleteService :execrows
DELETE FROM services
WHERE id = @id;

-- name: CreateIntegration :exec
INSERT INTO integrations (id, service_id, name, type, key_version, previous_key_valid_until, status, created_at, updated_at)
VALUES (@id, @service_id, @name, @type, @key_version, @previous_key_valid_until, @status, @created_at, @updated_at);

-- name: GetIntegration :one
SELECT id, service_id, name, type, key_version, previous_key_valid_until, status, created_at, updated_at
FROM integrations
WHERE id = @id;

-- name: ListIntegrations :many
SELECT id, service_id, name, type, key_version, previous_key_valid_until, status, created_at, updated_at
FROM integrations
WHERE service_id = @service_id AND id < @before
ORDER BY id DESC
LIMIT @max_rows;

-- name: UpdateIntegration :execrows
UPDATE integrations
SET name = @name, status = @status, updated_at = @updated_at
WHERE id = @id;

-- name: DeleteIntegration :execrows
DELETE FROM integrations
WHERE id = @id;

-- name: RotateIntegrationKey :execrows
UPDATE integrations
SET key_version = key_version + 1, previous_key_valid_until = @previous_key_valid_until, updated_at = @updated_at
WHERE id = @id;

-- name: PolicyInUse :one
SELECT EXISTS (
  SELECT 1 FROM services WHERE escalation_policy_id = @escalation_policy_id
) AS in_use;

-- name: GetIntegrationAndService :one
SELECT sqlc.embed(i), sqlc.embed(s)
FROM integrations AS i
JOIN services AS s ON s.id = i.service_id
WHERE i.id = @id;
