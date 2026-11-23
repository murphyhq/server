-- name: MarkHandled :execrows
INSERT IGNORE INTO handled_messages (id, created_at)
VALUES (sqlc.arg(id), sqlc.arg(created_at));

-- name: PurgeHandledMessages :execrows
DELETE FROM handled_messages
WHERE created_at < sqlc.arg(before);

-- name: FindOpenIncident :one
SELECT id, number, service_id, dedup_key, title, status, urgency, version, created_at, updated_at, resolved_at
FROM incidents
WHERE service_id = sqlc.arg(service_id) AND dedup_key = sqlc.arg(dedup_key) AND status <> 'resolved';

-- name: CreateIncident :execresult
INSERT INTO incidents (id, service_id, dedup_key, title, status, urgency, version, created_at, updated_at, resolved_at)
VALUES (sqlc.arg(id), sqlc.arg(service_id), sqlc.arg(dedup_key), sqlc.arg(title), sqlc.arg(status), sqlc.arg(urgency), sqlc.arg(version), sqlc.arg(created_at), sqlc.arg(updated_at), sqlc.narg(resolved_at));

-- name: UpdateIncident :execrows
UPDATE incidents
SET status = sqlc.arg(status), resolved_at = sqlc.narg(resolved_at), updated_at = sqlc.arg(updated_at), version = version + 1
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version);

-- name: GetIncident :one
SELECT id, number, service_id, dedup_key, title, status, urgency, version, created_at, updated_at, resolved_at
FROM incidents
WHERE id = sqlc.arg(id);

-- name: ListIncidents :many
SELECT id, number, service_id, dedup_key, title, status, urgency, version, created_at, updated_at, resolved_at
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
INSERT INTO timeline_entries (id, incident_id, type, alert_id, summary, created_at)
VALUES (sqlc.arg(id), sqlc.arg(incident_id), sqlc.arg(type), sqlc.narg(alert_id), sqlc.arg(summary), sqlc.arg(created_at));

-- name: ListTimeline :many
SELECT id, incident_id, type, alert_id, summary, created_at
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
