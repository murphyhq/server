-- +goose Up
CREATE TABLE services (
  id uuid NOT NULL,
  name text NOT NULL,
  description text NOT NULL,
  escalation_policy_id uuid NOT NULL,
  urgency text NOT NULL,
  status text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT services_pkey PRIMARY KEY (id),
  CONSTRAINT services_name_key UNIQUE (name),
  CONSTRAINT services_urgency_check CHECK (urgency IN ('high', 'low')),
  CONSTRAINT services_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE TABLE integrations (
  id uuid NOT NULL,
  service_id uuid NOT NULL,
  name text NOT NULL,
  type text NOT NULL,
  key_version integer NOT NULL,
  previous_key_valid_until timestamptz,
  status text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT integrations_pkey PRIMARY KEY (id),
  CONSTRAINT integrations_service_id_fkey FOREIGN KEY (service_id) REFERENCES services (id) ON DELETE CASCADE,
  CONSTRAINT integrations_type_check CHECK (type IN ('generic', 'alertmanager', 'grafana', 'datadog', 'cloudwatch', 'google_cloud')),
  CONSTRAINT integrations_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE INDEX services_escalation_policy_id_idx ON services (escalation_policy_id);

CREATE INDEX integrations_service_id_id_idx ON integrations (service_id, id);
