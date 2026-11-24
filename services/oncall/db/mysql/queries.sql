-- name: CreateSchedule :exec
INSERT INTO schedules (id, name, time_zone, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(name), sqlc.arg(time_zone), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: GetSchedule :one
SELECT id, name, time_zone, created_at, updated_at
FROM schedules
WHERE id = sqlc.arg(id);

-- name: ListSchedules :many
SELECT s.id, s.name, s.time_zone, s.created_at, s.updated_at
FROM schedules AS s
WHERE s.id < sqlc.arg(before) AND (sqlc.narg(team_id) IS NULL OR EXISTS (SELECT 1 FROM schedule_teams AS st WHERE st.schedule_id = s.id AND st.team_id = sqlc.narg(team_id)))
ORDER BY s.id DESC
LIMIT ?;

-- name: UpdateSchedule :execrows
UPDATE schedules
SET name = sqlc.arg(name), time_zone = sqlc.arg(time_zone), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: DeleteSchedule :execrows
DELETE FROM schedules
WHERE id = sqlc.arg(id);

-- name: DeleteScheduleTeams :exec
DELETE FROM schedule_teams
WHERE schedule_id = sqlc.arg(schedule_id);

-- name: ListScheduleTeams :many
SELECT schedule_id, team_id
FROM schedule_teams
WHERE schedule_id IN (sqlc.slice(schedule_ids))
ORDER BY schedule_id, team_id;

-- name: DeleteLayers :exec
DELETE FROM layers
WHERE schedule_id = sqlc.arg(schedule_id);

-- name: ListLayers :many
SELECT id, schedule_id, position, name, turn_length_seconds, starts_at
FROM layers
WHERE schedule_id IN (sqlc.slice(schedule_ids))
ORDER BY schedule_id, position;

-- name: ListLayerParticipants :many
SELECT lp.layer_id, lp.user_id
FROM layer_participants AS lp
JOIN layers AS l ON l.id = lp.layer_id
WHERE l.schedule_id IN (sqlc.slice(schedule_ids))
ORDER BY lp.layer_id, lp.position;

-- name: CreateOverride :exec
INSERT INTO overrides (id, schedule_id, user_id, starts_at, ends_at, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(schedule_id), sqlc.arg(user_id), sqlc.arg(starts_at), sqlc.arg(ends_at), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: ListOverrides :many
SELECT id, schedule_id, user_id, starts_at, ends_at, created_at, updated_at
FROM overrides
WHERE schedule_id = sqlc.arg(schedule_id) AND ends_at > sqlc.arg(ends_after)
ORDER BY starts_at, id;

-- name: DeleteOverride :execrows
DELETE FROM overrides
WHERE id = sqlc.arg(id);

-- name: ScheduleNames :many
SELECT id, name
FROM schedules
WHERE id IN (sqlc.slice(ids));

-- name: GetSchedules :many
SELECT id, name, time_zone, created_at, updated_at
FROM schedules
WHERE id IN (sqlc.slice(ids));

-- name: ListOverridesAt :many
SELECT id, schedule_id, user_id, starts_at, ends_at, created_at, updated_at
FROM overrides
WHERE schedule_id IN (sqlc.slice(schedule_ids)) AND starts_at <= sqlc.arg(at) AND ends_at > sqlc.arg(after)
ORDER BY schedule_id, starts_at, id;

-- name: GetOverride :one
SELECT id, schedule_id, user_id, starts_at, ends_at, created_at, updated_at
FROM overrides
WHERE id = sqlc.arg(id);
