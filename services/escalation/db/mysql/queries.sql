-- name: CreatePolicy :exec
INSERT INTO escalation_policies (id, name, team_id, version, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(team_id), sqlc.arg(version), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: CreateVersion :exec
INSERT INTO escalation_policy_versions (id, policy_id, version, repeat_count, created_at)
VALUES (sqlc.arg(id), sqlc.arg(policy_id), sqlc.arg(version), sqlc.arg(repeat_count), sqlc.arg(created_at));

-- name: GetPolicy :one
SELECT p.id, p.name, p.team_id, p.version, v.id AS version_id, v.repeat_count, p.created_at, p.updated_at
FROM escalation_policies AS p
JOIN escalation_policy_versions AS v ON v.policy_id = p.id AND v.version = p.version
WHERE p.id = sqlc.arg(id) AND p.deleted_at IS NULL;

-- name: GetPolicyVersion :one
SELECT p.id, p.name, p.team_id, v.version, v.id AS version_id, v.repeat_count, p.created_at, p.updated_at
FROM escalation_policies AS p
JOIN escalation_policy_versions AS v ON v.policy_id = p.id
WHERE p.id = sqlc.arg(id) AND v.version = sqlc.arg(version);

-- name: ListPolicies :many
SELECT p.id, p.name, p.team_id, p.version, v.id AS version_id, v.repeat_count, p.created_at, p.updated_at
FROM escalation_policies AS p
JOIN escalation_policy_versions AS v ON v.policy_id = p.id AND v.version = p.version
WHERE p.id < sqlc.arg(before) AND p.deleted_at IS NULL AND (sqlc.narg(team_id) IS NULL OR p.team_id = sqlc.narg(team_id))
ORDER BY p.id DESC
LIMIT ?;

-- name: UpdatePolicy :execrows
UPDATE escalation_policies
SET name = sqlc.arg(name), team_id = sqlc.arg(team_id), version = sqlc.arg(version), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(previous_version) AND deleted_at IS NULL;

-- name: DeletePolicy :execrows
UPDATE escalation_policies
SET deleted_at = sqlc.arg(deleted_at)
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: ListLevels :many
SELECT id, version_id, position, delay_seconds
FROM escalation_levels
WHERE version_id IN (sqlc.slice(version_ids))
ORDER BY version_id, position;

-- name: ListLevelTargets :many
SELECT lt.level_id, lt.target_type, lt.target_id
FROM level_targets AS lt
JOIN escalation_levels AS l ON l.id = lt.level_id
WHERE l.version_id IN (sqlc.slice(version_ids))
ORDER BY lt.level_id, lt.position;
