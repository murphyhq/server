-- +goose Up
ALTER TABLE integrations
  DROP CHECK integrations_type_check,
  ADD CONSTRAINT integrations_type_check CHECK (type IN ('generic', 'alertmanager', 'grafana', 'datadog', 'cloudwatch', 'google_cloud'));
