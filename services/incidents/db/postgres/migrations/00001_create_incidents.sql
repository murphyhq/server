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
  alert_id uuid,
  summary text NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT timeline_entries_pkey PRIMARY KEY (id),
  CONSTRAINT timeline_entries_incident_id_fkey FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE CASCADE,
  CONSTRAINT timeline_entries_type_check CHECK (type IN ('triggered', 'alerted', 'acknowledged', 'resolved'))
);

CREATE INDEX timeline_entries_incident_id_id_idx ON timeline_entries (incident_id, id);

CREATE TABLE handled_messages (
  id uuid NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT handled_messages_pkey PRIMARY KEY (id)
);

CREATE INDEX handled_messages_created_at_idx ON handled_messages (created_at);
