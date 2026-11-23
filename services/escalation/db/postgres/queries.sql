-- name: CreatePolicy :exec
INSERT INTO escalation_policies (id, name, team_id, repeat_count, created_at, updated_at)
VALUES (@id, @name, @team_id, @repeat_count, @created_at, @updated_at);

-- name: GetPolicy :one
SELECT id, name, team_id, repeat_count, created_at, updated_at
FROM escalation_policies
WHERE id = @id;

-- name: ListPolicies :many
SELECT id, name, team_id, repeat_count, created_at, updated_at
FROM escalation_policies
WHERE id < @before AND (sqlc.narg(team_id)::uuid IS NULL OR team_id = sqlc.narg(team_id))
ORDER BY id DESC
LIMIT @max_rows;

-- name: UpdatePolicy :execrows
UPDATE escalation_policies
SET name = @name, team_id = @team_id, repeat_count = @repeat_count, updated_at = @updated_at
WHERE id = @id;

-- name: DeletePolicy :execrows
DELETE FROM escalation_policies
WHERE id = @id;

-- name: DeleteLevels :exec
DELETE FROM escalation_levels
WHERE policy_id = @policy_id;

-- name: CreateLevels :exec
INSERT INTO escalation_levels (id, policy_id, position, delay_seconds, created_at)
SELECT unnest(@ids::uuid[]), @policy_id::uuid, unnest(@positions::integer[]), unnest(@delay_seconds::bigint[]), @created_at::timestamptz;

-- name: CreateLevelTargets :exec
INSERT INTO level_targets (id, level_id, position, target_type, target_id, created_at)
SELECT unnest(@ids::uuid[]), unnest(@level_ids::uuid[]), unnest(@positions::integer[]), unnest(@target_types::text[]), unnest(@target_ids::uuid[]), @created_at::timestamptz;

-- name: ListLevels :many
SELECT id, policy_id, position, delay_seconds
FROM escalation_levels
WHERE policy_id = ANY(@policy_ids::uuid[])
ORDER BY policy_id, position;

-- name: ListLevelTargets :many
SELECT lt.level_id, lt.target_type, lt.target_id
FROM level_targets AS lt
JOIN escalation_levels AS l ON l.id = lt.level_id
WHERE l.policy_id = ANY(@policy_ids::uuid[])
ORDER BY lt.level_id, lt.position;
