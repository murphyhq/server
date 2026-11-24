-- +goose Up
CREATE TABLE incident_pages (
  id CHAR(36) NOT NULL,
  incident_id CHAR(36) NOT NULL,
  user_id CHAR(36) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT incident_pages_pkey PRIMARY KEY (id),
  CONSTRAINT incident_pages_incident_id_fkey FOREIGN KEY (incident_id) REFERENCES incidents (id) ON DELETE CASCADE
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX incident_pages_user_id_incident_id_idx ON incident_pages (user_id, incident_id);
