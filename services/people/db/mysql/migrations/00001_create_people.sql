-- +goose Up
CREATE TABLE users (
  id CHAR(36) NOT NULL,
  name VARCHAR(255) NOT NULL,
  email VARCHAR(320) NOT NULL,
  time_zone VARCHAR(64) NOT NULL,
  status VARCHAR(16) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT users_pkey PRIMARY KEY (id),
  CONSTRAINT users_email_key UNIQUE (email),
  CONSTRAINT users_status_check CHECK (status IN ('active', 'deactivated'))
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE TABLE contact_methods (
  id CHAR(36) NOT NULL,
  user_id CHAR(36) NOT NULL,
  type VARCHAR(16) NOT NULL,
  address VARCHAR(2048) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT contact_methods_pkey PRIMARY KEY (id),
  CONSTRAINT contact_methods_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT contact_methods_type_check CHECK (type IN ('email', 'sms', 'voice', 'webhook', 'push'))
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX contact_methods_user_id_id_idx ON contact_methods (user_id, id);

CREATE TABLE teams (
  id CHAR(36) NOT NULL,
  name VARCHAR(255) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT teams_pkey PRIMARY KEY (id),
  CONSTRAINT teams_name_key UNIQUE (name)
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE TABLE team_memberships (
  id CHAR(36) NOT NULL,
  team_id CHAR(36) NOT NULL,
  user_id CHAR(36) NOT NULL,
  role VARCHAR(16) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT team_memberships_pkey PRIMARY KEY (id),
  CONSTRAINT team_memberships_team_id_user_id_key UNIQUE (team_id, user_id),
  CONSTRAINT team_memberships_team_id_fkey FOREIGN KEY (team_id) REFERENCES teams (id) ON DELETE CASCADE,
  CONSTRAINT team_memberships_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT team_memberships_role_check CHECK (role IN ('manager', 'responder', 'observer'))
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX team_memberships_user_id_idx ON team_memberships (user_id);
