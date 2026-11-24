-- name: MarkHandled :execrows
INSERT IGNORE INTO handled_messages (id, created_at)
VALUES (sqlc.arg(id), sqlc.arg(created_at));

-- name: PurgeHandledMessages :execrows
DELETE FROM handled_messages
WHERE created_at < sqlc.arg(before);

-- name: CreatePage :exec
INSERT INTO pages (id, incident_id, escalation_level, escalation_round, created_at)
VALUES (sqlc.arg(id), sqlc.arg(incident_id), sqlc.arg(escalation_level), sqlc.arg(escalation_round), sqlc.arg(created_at));

-- name: UpdateNotificationOutcome :exec
UPDATE notifications
SET status = sqlc.arg(status), failure = sqlc.narg(failure), sent_at = sqlc.narg(sent_at), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: RecipientsOf :many
SELECT DISTINCT user_id
FROM page_recipients
WHERE incident_id = sqlc.arg(incident_id)
ORDER BY user_id;

-- name: LockDueMessages :many
SELECT id, due_at, envelope
FROM scheduled_messages
WHERE due_at <= sqlc.arg(now) AND (claimed_until IS NULL OR claimed_until <= sqlc.arg(claims_expire_by))
ORDER BY due_at, id
LIMIT ?
FOR UPDATE SKIP LOCKED;

-- name: ClaimMessages :exec
UPDATE scheduled_messages
SET claimed_until = sqlc.arg(claimed_until)
WHERE id IN (sqlc.slice(ids));

-- name: DeleteMessages :exec
DELETE FROM scheduled_messages
WHERE id IN (sqlc.slice(ids));
