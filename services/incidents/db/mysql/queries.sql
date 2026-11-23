-- name: MarkHandled :execrows
INSERT IGNORE INTO handled_messages (id, created_at)
VALUES (sqlc.arg(id), sqlc.arg(created_at));

-- name: PurgeHandledMessages :execrows
DELETE FROM handled_messages
WHERE created_at < sqlc.arg(before);

-- name: FindOpenIncident :one
SELECT id, number, service_id, dedup_key, title, status, urgency, version, created_at, updated_at, resolved_at, escalation_policy_id, escalation_policy_version, escalation_level, escalation_round, escalation_exhausted, acknowledged_at
FROM incidents
WHERE service_id = sqlc.arg(service_id) AND dedup_key = sqlc.arg(dedup_key) AND status <> 'resolved';

-- name: CreateIncident :execresult
INSERT INTO incidents (id, service_id, dedup_key, title, status, urgency, version, created_at, updated_at, resolved_at, escalation_policy_id, escalation_policy_version, escalation_level, escalation_round, escalation_exhausted, acknowledged_at)
VALUES (sqlc.arg(id), sqlc.arg(service_id), sqlc.arg(dedup_key), sqlc.arg(title), sqlc.arg(status), sqlc.arg(urgency), sqlc.arg(version), sqlc.arg(created_at), sqlc.arg(updated_at), sqlc.narg(resolved_at), sqlc.arg(escalation_policy_id), sqlc.arg(escalation_policy_version), sqlc.arg(escalation_level), sqlc.arg(escalation_round), sqlc.arg(escalation_exhausted), sqlc.narg(acknowledged_at));

-- name: UpdateIncident :execrows
UPDATE incidents
SET status = sqlc.arg(status), resolved_at = sqlc.narg(resolved_at), acknowledged_at = sqlc.narg(acknowledged_at), escalation_level = sqlc.arg(escalation_level), escalation_round = sqlc.arg(escalation_round), escalation_exhausted = sqlc.arg(escalation_exhausted), updated_at = sqlc.arg(updated_at), version = sqlc.arg(version)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(previous_version);

-- name: GetIncident :one
SELECT id, number, service_id, dedup_key, title, status, urgency, version, created_at, updated_at, resolved_at, escalation_policy_id, escalation_policy_version, escalation_level, escalation_round, escalation_exhausted, acknowledged_at
FROM incidents
WHERE id = sqlc.arg(id);

-- name: ListIncidents :many
SELECT id, number, service_id, dedup_key, title, status, urgency, version, created_at, updated_at, resolved_at, escalation_policy_id, escalation_policy_version, escalation_level, escalation_round, escalation_exhausted, acknowledged_at
FROM incidents
WHERE id < sqlc.arg(before)
  AND (sqlc.arg(any_status) OR status IN (sqlc.slice(statuses)))
  AND (sqlc.arg(any_service) OR service_id IN (sqlc.slice(service_ids)))
ORDER BY id DESC
LIMIT ?;

-- name: CreateAlert :exec
INSERT INTO alerts (id, incident_id, integration_id, service_id, dedup_key, status, summary, labels, received_at, created_at, source, instance, annotations)
VALUES (sqlc.arg(id), sqlc.narg(incident_id), sqlc.arg(integration_id), sqlc.arg(service_id), sqlc.arg(dedup_key), sqlc.arg(status), sqlc.arg(summary), sqlc.arg(labels), sqlc.arg(received_at), sqlc.arg(created_at), sqlc.arg(source), sqlc.narg(instance), sqlc.arg(annotations));

-- name: LatestFiringAt :one
SELECT received_at
FROM alerts
WHERE incident_id = sqlc.arg(incident_id) AND status = 'firing'
ORDER BY received_at DESC
LIMIT 1;

-- name: CreateTimelineEntry :exec
INSERT INTO timeline_entries (id, incident_id, type, summary, details, created_at)
VALUES (sqlc.arg(id), sqlc.arg(incident_id), sqlc.arg(type), sqlc.arg(summary), sqlc.arg(details), sqlc.arg(created_at));

-- name: ListTimeline :many
SELECT id, details
FROM timeline_entries
WHERE incident_id = sqlc.arg(incident_id)
ORDER BY id;

-- name: OpenInstances :many
SELECT DISTINCT fired.instance
FROM alerts AS fired
WHERE fired.incident_id = sqlc.arg(incident_id) AND fired.status = 'firing' AND fired.instance IS NOT NULL
  AND fired.instance NOT IN (
    SELECT resolved.instance FROM alerts AS resolved
    WHERE resolved.incident_id = fired.incident_id AND resolved.status = 'resolved' AND resolved.instance IS NOT NULL
  );




-- name: DeleteTimers :exec
DELETE FROM scheduled_messages
WHERE incident_id = sqlc.arg(incident_id) AND kind = 'timer';

-- name: LockDueMessages :many
SELECT id, incident_id, kind, due_at, envelope
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
