-- +goose Up
CREATE TABLE services (
  id CHAR(36) NOT NULL,
  name VARCHAR(255) NOT NULL,
  description TEXT NOT NULL,
  team_id CHAR(36) NOT NULL,
  escalation_policy_id CHAR(36) NOT NULL,
  urgency VARCHAR(16) NOT NULL,
  status VARCHAR(16) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT services_pkey PRIMARY KEY (id),
  CONSTRAINT services_name_key UNIQUE (name),
  CONSTRAINT services_urgency_check CHECK (urgency IN ('high', 'low')),
  CONSTRAINT services_status_check CHECK (status IN ('active', 'disabled'))
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE TABLE integrations (
  id CHAR(36) NOT NULL,
  service_id CHAR(36) NOT NULL,
  name VARCHAR(255) NOT NULL,
  type VARCHAR(32) NOT NULL,
  key_version INT NOT NULL,
  previous_key_valid_until DATETIME(6),
  status VARCHAR(16) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT integrations_pkey PRIMARY KEY (id),
  CONSTRAINT integrations_service_id_fkey FOREIGN KEY (service_id) REFERENCES services (id) ON DELETE CASCADE,
  CONSTRAINT integrations_type_check CHECK (type IN ('generic', 'alertmanager', 'grafana', 'datadog')),
  CONSTRAINT integrations_status_check CHECK (status IN ('active', 'disabled'))
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX integrations_service_id_id_idx ON integrations (service_id, id);
