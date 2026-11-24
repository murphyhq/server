-- name: CreatePolicy :exec
INSERT INTO escalation_policies (id, name, team_id, version, created_at, updated_at)
VALUES (@id, @name, @team_id, @version, @created_at, @updated_at);

-- name: CreateVersion :exec
INSERT INTO escalation_policy_versions (id, policy_id, version, repeat_count, created_at)
VALUES (@id, @policy_id, @version, @repeat_count, @created_at);

-- name: GetPolicy :one
SELECT p.id, p.name, p.team_id, p.version, v.id AS version_id, v.repeat_count, p.created_at, p.updated_at
FROM escalation_policies AS p
JOIN escalation_policy_versions AS v ON v.policy_id = p.id AND v.version = p.version
WHERE p.id = @id AND p.deleted_at IS NULL;

-- name: GetPolicyVersion :one
SELECT p.id, p.name, p.team_id, v.version, v.id AS version_id, v.repeat_count, p.created_at, p.updated_at
FROM escalation_policies AS p
JOIN escalation_policy_versions AS v ON v.policy_id = p.id
WHERE p.id = @id AND v.version = @version;

-- name: ListPolicies :many
SELECT p.id, p.name, p.team_id, p.version, v.id AS version_id, v.repeat_count, p.created_at, p.updated_at
FROM escalation_policies AS p
JOIN escalation_policy_versions AS v ON v.policy_id = p.id AND v.version = p.version
WHERE p.id < @before AND p.deleted_at IS NULL AND (sqlc.narg(team_id)::uuid IS NULL OR p.team_id = sqlc.narg(team_id))
ORDER BY p.id DESC
LIMIT @max_rows;

-- name: UpdatePolicy :execrows
UPDATE escalation_policies
SET name = @name, team_id = @team_id, version = @version, updated_at = @updated_at
WHERE id = @id AND version = @previous_version AND deleted_at IS NULL;

-- name: DeletePolicy :execrows
UPDATE escalation_policies
SET deleted_at = @deleted_at
WHERE id = @id AND deleted_at IS NULL;

-- name: ListLevels :many
SELECT id, version_id, position, delay_seconds
FROM escalation_levels
WHERE version_id = ANY(@version_ids::uuid[])
ORDER BY version_id, position;

-- name: ListLevelTargets :many
SELECT lt.level_id, lt.target_type, lt.target_id
FROM level_targets AS lt
JOIN escalation_levels AS l ON l.id = lt.level_id
WHERE l.version_id = ANY(@version_ids::uuid[])
ORDER BY lt.level_id, lt.position;

-- name: CreateLevels :exec
INSERT INTO escalation_levels (id, version_id, position, delay_seconds, created_at)
SELECT unnest(@ids::uuid[]), @version_id::uuid, unnest(@positions::integer[]), unnest(@delay_seconds::bigint[]), @created_at::timestamptz;

-- name: CreateLevelTargets :exec
INSERT INTO level_targets (id, level_id, position, target_type, target_id, created_at)
SELECT unnest(@ids::uuid[]), unnest(@level_ids::uuid[]), unnest(@positions::integer[]), unnest(@target_types::text[]), unnest(@target_ids::uuid[]), @created_at::timestamptz;

-- name: CurrentVersion :one
SELECT version
FROM escalation_policies
WHERE id = @id AND deleted_at IS NULL;

-- name: PolicyNames :many
SELECT id, name
FROM escalation_policies
WHERE id = ANY(@ids::uuid[]);

-- name: PoliciesOfTeams :many
SELECT id, name
FROM escalation_policies
WHERE team_id = ANY(@team_ids::uuid[]);
