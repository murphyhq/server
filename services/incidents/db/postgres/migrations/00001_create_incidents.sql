-- +goose Up
CREATE TABLE incidents (
  id uuid NOT NULL,
  number bigint NOT NULL GENERATED ALWAYS AS IDENTITY,
  service_id uuid NOT NULL,
  dedup_key text NOT NULL,
  title text NOT NULL,
  status text NOT NULL,
  urgency text NOT NULL,
  version integer NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  resolved_at timestamptz,
  escalation_policy_id uuid NOT NULL,
  escalation_policy_version integer NOT NULL,
  escalation_level integer NOT NULL,
  escalation_round integer NOT NULL,
  escalation_exhausted boolean NOT NULL,
  acknowledged_at timestamptz,
  CONSTRAINT incidents_pkey PRIMARY KEY (id),
  CONSTRAINT incidents_number_key UNIQUE (number),
  CONSTRAINT incidents_status_check CHECK (status IN ('triggered', 'acknowledged', 'resolved')),
  CONSTRAINT incidents_urgency_check CHECK (urgency IN ('high', 'low'))
);

CREATE UNIQUE INDEX incidents_open_dedup_key_idx ON incidents (service_id, dedup_key) WHERE status <> 'resolved';
CREATE INDEX incidents_status_id_idx ON incidents (status, id);
CREATE INDEX incidents_service_id_id_idx ON incidents (service_id, id);

CREATE TABLE alerts (
  id uuid NOT NULL,
  incident_id uuid,
  integration_id uuid NOT NULL,
  service_id uuid NOT NULL,
  dedup_key text NOT NULL,
  status text NOT NULL,
  summary text NOT NULL,
  labels jsonb NOT NULL,
  annotations jsonb NOT NULL,
  source text NOT NULL,
  instance text,
  received_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT alerts_pkey PRIMARY KEY (id),
  CONSTRAINT alerts_incident_id_fkey FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE CASCADE,
  CONSTRAINT alerts_status_check CHECK (status IN ('firing', 'resolved'))
);

CREATE INDEX alerts_incident_id_id_idx ON alerts (incident_id, id);

CREATE TABLE timeline_entries (
  id uuid NOT NULL,
  incident_id uuid NOT NULL,
  type text NOT NULL,
  summary text NOT NULL,
  details bytea NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT timeline_entries_pkey PRIMARY KEY (id),
  CONSTRAINT timeline_entries_incident_id_fkey FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE CASCADE,
  CONSTRAINT timeline_entries_type_check CHECK (type IN ('triggered', 'alerted', 'acknowledged', 'resolved', 'escalated', 'escalation_skipped', 'escalation_exhausted', 'notified', 'notification_failed'))
);

CREATE INDEX timeline_entries_incident_id_id_idx ON timeline_entries (incident_id, id);

CREATE TABLE handled_messages (
  id uuid NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT handled_messages_pkey PRIMARY KEY (id)
);

CREATE INDEX handled_messages_created_at_idx ON handled_messages (created_at);

CREATE TABLE scheduled_messages (
  id uuid NOT NULL,
  incident_id uuid NOT NULL,
  kind text NOT NULL,
  due_at timestamptz NOT NULL,
  claimed_until timestamptz,
  envelope bytea NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT scheduled_messages_pkey PRIMARY KEY (id),
  CONSTRAINT scheduled_messages_incident_id_fkey FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE CASCADE,
  CONSTRAINT scheduled_messages_kind_check CHECK (kind IN ('event', 'timer'))
);

CREATE INDEX scheduled_messages_due_at_id_idx ON scheduled_messages (due_at, id);
CREATE INDEX scheduled_messages_incident_id_kind_idx ON scheduled_messages (incident_id, kind);

CREATE TABLE incident_pages (
  id uuid NOT NULL,
  incident_id uuid NOT NULL,
  user_id uuid NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT incident_pages_pkey PRIMARY KEY (id),
  CONSTRAINT incident_pages_incident_id_fkey FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE CASCADE
);

CREATE INDEX incident_pages_user_id_incident_id_idx ON incident_pages (user_id, incident_id);
CREATE INDEX incident_pages_incident_id_idx ON incident_pages (incident_id);
