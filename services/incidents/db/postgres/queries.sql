-- name: MarkHandled :execrows
INSERT INTO handled_messages (id, created_at)
VALUES (@id, @created_at)
ON CONFLICT (id) DO NOTHING;

-- name: PurgeHandledMessages :execrows
DELETE FROM handled_messages
WHERE created_at < @before;

-- name: FindOpenIncident :one
SELECT id, number, service_id, dedup_key, title, status, urgency, version, created_at, updated_at, resolved_at, escalation_policy_id, escalation_policy_version, escalation_level, escalation_round, escalation_exhausted, acknowledged_at
FROM incidents
WHERE service_id = @service_id AND dedup_key = @dedup_key AND status <> 'resolved';

-- name: CreateIncident :one
INSERT INTO incidents (id, service_id, dedup_key, title, status, urgency, version, created_at, updated_at, resolved_at, escalation_policy_id, escalation_policy_version, escalation_level, escalation_round, escalation_exhausted, acknowledged_at)
VALUES (@id, @service_id, @dedup_key, @title, @status, @urgency, @version, @created_at, @updated_at, @resolved_at, @escalation_policy_id, @escalation_policy_version, @escalation_level, @escalation_round, @escalation_exhausted, @acknowledged_at)
RETURNING number;

-- name: UpdateIncident :execrows
UPDATE incidents
SET status = @status, resolved_at = @resolved_at, acknowledged_at = @acknowledged_at, escalation_level = @escalation_level, escalation_round = @escalation_round, escalation_exhausted = @escalation_exhausted, updated_at = @updated_at, version = version + 1
WHERE id = @id AND version = @version;

-- name: GetIncident :one
SELECT id, number, service_id, dedup_key, title, status, urgency, version, created_at, updated_at, resolved_at, escalation_policy_id, escalation_policy_version, escalation_level, escalation_round, escalation_exhausted, acknowledged_at
FROM incidents
WHERE id = @id;

-- name: ListIncidents :many
SELECT id, number, service_id, dedup_key, title, status, urgency, version, created_at, updated_at, resolved_at, escalation_policy_id, escalation_policy_version, escalation_level, escalation_round, escalation_exhausted, acknowledged_at
FROM incidents
WHERE id < @before
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
  AND (coalesce(cardinality(@service_ids::uuid[]), 0) = 0 OR service_id = ANY(@service_ids::uuid[]))
ORDER BY id DESC
LIMIT @max_rows;

-- name: CreateAlert :exec
INSERT INTO alerts (id, incident_id, integration_id, service_id, dedup_key, status, summary, labels, received_at, created_at, source, instance, annotations)
VALUES (@id, @incident_id, @integration_id, @service_id, @dedup_key, @status, @summary, @labels, @received_at, @created_at, @source, @instance, @annotations);

-- name: CreateTimelineEntry :exec
INSERT INTO timeline_entries (id, incident_id, type, summary, details, created_at)
VALUES (@id, @incident_id, @type, @summary, @details, @created_at);

-- name: ListTimeline :many
SELECT id, details
FROM timeline_entries
WHERE incident_id = @incident_id
ORDER BY id;

-- name: LatestFiringAt :one
SELECT COALESCE(max(received_at), 'epoch'::timestamptz)::timestamptz AS received_at
FROM alerts
WHERE incident_id = @incident_id AND status = 'firing';

-- name: OpenInstances :many
SELECT DISTINCT fired.instance::text AS instance
FROM alerts AS fired
WHERE fired.incident_id = @incident_id AND fired.status = 'firing' AND fired.instance IS NOT NULL
  AND fired.instance NOT IN (
    SELECT resolved.instance FROM alerts AS resolved
    WHERE resolved.incident_id = fired.incident_id AND resolved.status = 'resolved' AND resolved.instance IS NOT NULL
  );




-- name: DeleteTimers :exec
DELETE FROM scheduled_messages
WHERE incident_id = @incident_id AND kind = 'timer';

-- name: LockDueMessages :many
SELECT id, incident_id, kind, due_at, envelope
FROM scheduled_messages
WHERE due_at <= @now AND (claimed_until IS NULL OR claimed_until <= @claims_expire_by)
ORDER BY due_at, id
LIMIT @max_rows
FOR UPDATE SKIP LOCKED;

-- name: ClaimMessages :exec
UPDATE scheduled_messages
SET claimed_until = @claimed_until
WHERE id = ANY(@ids::uuid[]);

-- name: DeleteMessages :exec
DELETE FROM scheduled_messages
WHERE id = ANY(@ids::uuid[]);




-- name: CreateScheduledMessages :exec
INSERT INTO scheduled_messages (id, incident_id, kind, due_at, envelope, created_at)
SELECT unnest(@ids::uuid[]), unnest(@incident_ids::uuid[]), unnest(@kinds::text[]), unnest(@due_ats::timestamptz[]), unnest(@envelopes::bytea[]), @created_at::timestamptz;
