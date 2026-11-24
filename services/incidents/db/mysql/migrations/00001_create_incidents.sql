-- +goose Up
CREATE TABLE incidents (
  id CHAR(36) NOT NULL,
  number BIGINT NOT NULL AUTO_INCREMENT,
  service_id CHAR(36) NOT NULL,
  dedup_key VARCHAR(255) NOT NULL,
  title TEXT NOT NULL,
  status VARCHAR(16) NOT NULL,
  urgency VARCHAR(16) NOT NULL,
  version INT NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  resolved_at DATETIME(6),
  escalation_policy_id CHAR(36) NOT NULL,
  escalation_policy_version INT NOT NULL,
  escalation_level INT NOT NULL,
  escalation_round INT NOT NULL,
  escalation_exhausted BOOLEAN NOT NULL,
  acknowledged_at DATETIME(6),
  open_service_id CHAR(36) AS (CASE WHEN status <> 'resolved' THEN service_id END) STORED,
  CONSTRAINT incidents_pkey PRIMARY KEY (id),
  CONSTRAINT incidents_number_key UNIQUE (number),
  CONSTRAINT incidents_open_dedup_key_idx UNIQUE (open_service_id, dedup_key),
  CONSTRAINT incidents_status_check CHECK (status IN ('triggered', 'acknowledged', 'resolved')),
  CONSTRAINT incidents_urgency_check CHECK (urgency IN ('high', 'low'))
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX incidents_status_id_idx ON incidents (status, id);
CREATE INDEX incidents_service_id_id_idx ON incidents (service_id, id);
CREATE INDEX incidents_service_id_dedup_key_idx ON incidents (service_id, dedup_key);

CREATE TABLE alerts (
  id CHAR(36) NOT NULL,
  incident_id CHAR(36),
  integration_id CHAR(36) NOT NULL,
  service_id CHAR(36) NOT NULL,
  dedup_key VARCHAR(255) NOT NULL,
  status VARCHAR(16) NOT NULL,
  summary TEXT NOT NULL,
  labels JSON NOT NULL,
  annotations JSON NOT NULL,
  source VARCHAR(32) NOT NULL,
  instance VARCHAR(512),
  received_at DATETIME(6) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT alerts_pkey PRIMARY KEY (id),
  CONSTRAINT alerts_incident_id_fkey FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE CASCADE,
  CONSTRAINT alerts_status_check CHECK (status IN ('firing', 'resolved'))
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX alerts_incident_id_id_idx ON alerts (incident_id, id);

CREATE TABLE timeline_entries (
  id CHAR(36) NOT NULL,
  incident_id CHAR(36) NOT NULL,
  type VARCHAR(32) NOT NULL,
  summary TEXT NOT NULL,
  details LONGBLOB NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT timeline_entries_pkey PRIMARY KEY (id),
  CONSTRAINT timeline_entries_incident_id_fkey FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE CASCADE,
  CONSTRAINT timeline_entries_type_check CHECK (type IN ('triggered', 'alerted', 'acknowledged', 'resolved', 'escalated', 'escalation_skipped', 'escalation_exhausted', 'notified', 'notification_failed'))
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX timeline_entries_incident_id_id_idx ON timeline_entries (incident_id, id);

CREATE TABLE handled_messages (
  id CHAR(36) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT handled_messages_pkey PRIMARY KEY (id)
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX handled_messages_created_at_idx ON handled_messages (created_at);

CREATE TABLE scheduled_messages (
  id CHAR(36) NOT NULL,
  incident_id CHAR(36) NOT NULL,
  kind VARCHAR(32) NOT NULL,
  due_at DATETIME(6) NOT NULL,
  claimed_until DATETIME(6),
  envelope LONGBLOB NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT scheduled_messages_pkey PRIMARY KEY (id),
  CONSTRAINT scheduled_messages_incident_id_fkey FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE CASCADE,
  CONSTRAINT scheduled_messages_kind_check CHECK (kind IN ('event', 'timer'))
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX scheduled_messages_due_at_id_idx ON scheduled_messages (due_at, id);
CREATE INDEX scheduled_messages_incident_id_kind_idx ON scheduled_messages (incident_id, kind);

CREATE TABLE incident_pages (
  id CHAR(36) NOT NULL,
  incident_id CHAR(36) NOT NULL,
  user_id CHAR(36) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT incident_pages_pkey PRIMARY KEY (id),
  CONSTRAINT incident_pages_incident_id_fkey FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE CASCADE
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX incident_pages_user_id_incident_id_idx ON incident_pages (user_id, incident_id);
