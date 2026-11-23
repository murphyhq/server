-- name: CreateSchedule :exec
INSERT INTO schedules (id, name, time_zone, created_at, updated_at)
VALUES (@id, @name, @time_zone, @created_at, @updated_at);

-- name: GetSchedule :one
SELECT id, name, time_zone, created_at, updated_at
FROM schedules
WHERE id = @id;

-- name: ListSchedules :many
SELECT s.id, s.name, s.time_zone, s.created_at, s.updated_at
FROM schedules AS s
WHERE s.id < @before AND (sqlc.narg(team_id)::uuid IS NULL OR EXISTS (SELECT 1 FROM schedule_teams AS st WHERE st.schedule_id = s.id AND st.team_id = sqlc.narg(team_id)))
ORDER BY s.id DESC
LIMIT @max_rows;

-- name: UpdateSchedule :execrows
UPDATE schedules
SET name = @name, time_zone = @time_zone, updated_at = @updated_at
WHERE id = @id;

-- name: DeleteSchedule :execrows
DELETE FROM schedules
WHERE id = @id;

-- name: DeleteScheduleTeams :exec
DELETE FROM schedule_teams
WHERE schedule_id = @schedule_id;

-- name: CreateScheduleTeams :exec
INSERT INTO schedule_teams (id, schedule_id, team_id, created_at)
SELECT unnest(@ids::uuid[]), @schedule_id::uuid, unnest(@team_ids::uuid[]), @created_at::timestamptz;

-- name: ListScheduleTeams :many
SELECT schedule_id, team_id
FROM schedule_teams
WHERE schedule_id = ANY(@schedule_ids::uuid[])
ORDER BY schedule_id, team_id;

-- name: DeleteLayers :exec
DELETE FROM layers
WHERE schedule_id = @schedule_id;

-- name: CreateLayers :exec
INSERT INTO layers (id, schedule_id, position, name, turn_length_seconds, starts_at, created_at)
SELECT unnest(@ids::uuid[]), @schedule_id::uuid, unnest(@positions::integer[]), unnest(@names::text[]), unnest(@turn_length_seconds::bigint[]), unnest(@starts_at::timestamptz[]), @created_at::timestamptz;

-- name: CreateLayerParticipants :exec
INSERT INTO layer_participants (id, layer_id, position, user_id, created_at)
SELECT unnest(@ids::uuid[]), unnest(@layer_ids::uuid[]), unnest(@positions::integer[]), unnest(@user_ids::uuid[]), @created_at::timestamptz;

-- name: ListLayers :many
SELECT id, schedule_id, position, name, turn_length_seconds, starts_at
FROM layers
WHERE schedule_id = ANY(@schedule_ids::uuid[])
ORDER BY schedule_id, position;

-- name: ListLayerParticipants :many
SELECT lp.layer_id, lp.user_id
FROM layer_participants AS lp
JOIN layers AS l ON l.id = lp.layer_id
WHERE l.schedule_id = ANY(@schedule_ids::uuid[])
ORDER BY lp.layer_id, lp.position;

-- name: CreateOverride :exec
INSERT INTO overrides (id, schedule_id, user_id, starts_at, ends_at, created_at, updated_at)
VALUES (@id, @schedule_id, @user_id, @starts_at, @ends_at, @created_at, @updated_at);

-- name: ListOverrides :many
SELECT id, schedule_id, user_id, starts_at, ends_at, created_at, updated_at
FROM overrides
WHERE schedule_id = @schedule_id AND ends_at > @ends_after
ORDER BY starts_at, id;

-- name: DeleteOverride :execrows
DELETE FROM overrides
WHERE id = @id;

-- name: ExistingSchedules :many
SELECT id
FROM schedules
WHERE id = ANY(@ids::uuid[]);
