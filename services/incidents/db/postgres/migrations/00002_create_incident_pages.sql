-- +goose Up
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
