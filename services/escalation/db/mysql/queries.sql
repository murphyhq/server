-- name: CreatePolicy :exec
INSERT INTO escalation_policies (id, name, team_id, repeat_count, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(team_id), sqlc.arg(repeat_count), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: GetPolicy :one
SELECT id, name, team_id, repeat_count, created_at, updated_at
FROM escalation_policies
WHERE id = sqlc.arg(id);

-- name: ListPolicies :many
SELECT id, name, team_id, repeat_count, created_at, updated_at
FROM escalation_policies
WHERE id < sqlc.arg(before) AND (sqlc.narg(team_id) IS NULL OR team_id = sqlc.narg(team_id))
ORDER BY id DESC
LIMIT ?;

-- name: UpdatePolicy :execrows
UPDATE escalation_policies
SET name = sqlc.arg(name), team_id = sqlc.arg(team_id), repeat_count = sqlc.arg(repeat_count), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: DeletePolicy :execrows
DELETE FROM escalation_policies
WHERE id = sqlc.arg(id);

-- name: DeleteLevels :exec
DELETE FROM escalation_levels
WHERE policy_id = sqlc.arg(policy_id);

-- name: ListLevels :many
SELECT id, policy_id, position, delay_seconds
FROM escalation_levels
WHERE policy_id IN (sqlc.slice(policy_ids))
ORDER BY policy_id, position;

-- name: ListLevelTargets :many
SELECT lt.level_id, lt.target_type, lt.target_id
FROM level_targets AS lt
JOIN escalation_levels AS l ON l.id = lt.level_id
WHERE l.policy_id IN (sqlc.slice(policy_ids))
ORDER BY lt.level_id, lt.position;
