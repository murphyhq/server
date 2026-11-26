-- name: MarkHandled :execrows
INSERT INTO handled_messages (id, created_at)
VALUES (@id, @created_at)
ON CONFLICT (id) DO NOTHING;

-- name: PurgeHandledMessages :execrows
DELETE FROM handled_messages
WHERE created_at < @before;

-- name: CreatePage :exec
INSERT INTO pages (id, incident_id, escalation_level, escalation_round, created_at)
VALUES (@id, @incident_id, @escalation_level, @escalation_round, @created_at);

-- name: CreatePageRecipients :exec
INSERT INTO page_recipients (id, page_id, incident_id, user_id, via_type, via_id, created_at)
SELECT unnest(@ids::uuid[]), @page_id::uuid, @incident_id::uuid, unnest(@user_ids::uuid[]), unnest(@via_types::text[]), unnest(@via_ids::uuid[]), @created_at::timestamptz;

-- name: CreateNotifications :exec
INSERT INTO notifications (id, subject, subject_id, page_id, user_id, contact_method_id, channel, reason, status, created_at, updated_at)
SELECT unnest(@ids::uuid[]), @subject::text, @subject_id::uuid, sqlc.narg(page_id)::uuid, unnest(@user_ids::uuid[]), unnest(@contact_method_ids::uuid[]), unnest(@channels::text[]), @reason::text, 'created', @created_at::timestamptz, @created_at::timestamptz;

-- name: UpdateNotificationOutcome :exec
UPDATE notifications
SET status = @status, failure = sqlc.narg(failure), sent_at = sqlc.narg(sent_at), updated_at = @updated_at
WHERE id = @id;

-- name: RecipientsOf :many
SELECT DISTINCT user_id
FROM page_recipients
WHERE incident_id = @incident_id
ORDER BY user_id;

-- name: CreateScheduledMessages :exec
INSERT INTO scheduled_messages (id, due_at, envelope, created_at, waiting_incident_id, waiting_notification_id)
SELECT unnest(@ids::uuid[]), unnest(@due_ats::timestamptz[]), unnest(@envelopes::bytea[]), @created_at::timestamptz,
  NULLIF(unnest(@waiting_incident_ids::text[]), '')::uuid, NULLIF(unnest(@waiting_notification_ids::text[]), '')::uuid;

-- name: LockDueMessages :many
SELECT id, due_at, envelope
FROM scheduled_messages
WHERE due_at <= @now AND (claimed_until IS NULL OR claimed_until <= @claims_expire_by)
ORDER BY due_at, id
LIMIT @max_rows
FOR UPDATE SKIP LOCKED;

-- name: ClaimMessages :exec
UPDATE scheduled_messages
SET claimed_until = @claimed_until
WHERE id = ANY(@ids::uuid[]);

-- name: CancelWaitingNotifications :exec
UPDATE notifications
SET status = 'cancelled', updated_at = @at
WHERE id IN (
  SELECT waiting_notification_id FROM scheduled_messages
  WHERE waiting_incident_id = @incident_id AND (claimed_until IS NULL OR claimed_until <= @at)
);

-- name: DeleteWaitingMessages :exec
DELETE FROM scheduled_messages
WHERE waiting_incident_id = @incident_id AND (claimed_until IS NULL OR claimed_until <= @at);

-- name: DeleteMessages :exec
DELETE FROM scheduled_messages
WHERE id = ANY(@ids::uuid[]);

-- name: RecordOutcome :one
WITH handled AS (
  INSERT INTO handled_messages (id, created_at)
  VALUES (@message_id, @at)
  ON CONFLICT (id) DO NOTHING
  RETURNING id
), updated AS (
  UPDATE notifications
  SET status = @status, failure = sqlc.narg(failure), sent_at = sqlc.narg(sent_at), updated_at = @at
  WHERE notifications.id = @id AND EXISTS (SELECT 1 FROM handled)
  RETURNING notifications.id
)
SELECT count(*) FROM handled;

-- name: RecordPage :one
WITH handled AS (
  INSERT INTO handled_messages (id, created_at)
  VALUES (@message_id, @at)
  ON CONFLICT (id) DO NOTHING
  RETURNING id
), page AS (
  INSERT INTO pages (id, incident_id, escalation_level, escalation_round, created_at)
  SELECT @page_id::uuid, @incident_id::uuid, @escalation_level::integer, @escalation_round::integer, @created_at::timestamptz
  FROM handled
  RETURNING id
), recipients AS (
  INSERT INTO page_recipients (id, page_id, incident_id, user_id, via_type, via_id, created_at)
  SELECT unnest(@recipient_ids::uuid[]), page.id, @incident_id::uuid, unnest(@recipient_user_ids::uuid[]), unnest(@recipient_via_types::text[]), unnest(@recipient_via_ids::uuid[]), @created_at::timestamptz
  FROM page
), created AS (
  INSERT INTO notifications (id, subject, subject_id, page_id, user_id, contact_method_id, channel, reason, status, created_at, updated_at)
  SELECT unnest(@notification_ids::uuid[]), 'incident', @incident_id::uuid, page.id, unnest(@notification_user_ids::uuid[]), unnest(@contact_method_ids::uuid[]), unnest(@channels::text[]), 'paged', 'created', @created_at::timestamptz, @created_at::timestamptz
  FROM page
), scheduled AS (
  INSERT INTO scheduled_messages (id, due_at, envelope, created_at, waiting_incident_id, waiting_notification_id)
  SELECT unnest(@message_ids::uuid[]), unnest(@due_ats::timestamptz[]), unnest(@envelopes::bytea[]), @at::timestamptz,
    NULLIF(unnest(@waiting_incident_ids::text[]), '')::uuid, NULLIF(unnest(@waiting_notification_ids::text[]), '')::uuid
  FROM handled
)
SELECT count(*) FROM handled;

-- name: GetEmailProvider :one
SELECT type, from_address, host, port, username, secret, updated_at, status, problem, checked_at
FROM email_provider;

-- name: SetEmailProvider :exec
INSERT INTO email_provider (type, from_address, host, port, username, secret, updated_at)
VALUES (@type, @from_address, @host, @port, @username, @secret, @updated_at)
ON CONFLICT (singleton) DO UPDATE
SET type = EXCLUDED.type, from_address = EXCLUDED.from_address, host = EXCLUDED.host, port = EXCLUDED.port,
  username = EXCLUDED.username, secret = EXCLUDED.secret, updated_at = EXCLUDED.updated_at,
  status = 'pending', problem = '', checked_at = NULL;

-- name: DeleteEmailProvider :exec
DELETE FROM email_provider;

-- name: RecordEmailProviderCheck :execrows
UPDATE email_provider
SET status = @status, problem = @problem, checked_at = @checked_at
WHERE updated_at = @updated_at;

-- name: ListTestNotifications :many
SELECT id, contact_method_id, channel, status, failure, created_at
FROM notifications
WHERE subject = 'test' AND subject_id = @test_id AND user_id = @user_id
ORDER BY id;
