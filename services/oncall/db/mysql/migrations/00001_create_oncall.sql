-- +goose Up
CREATE TABLE schedules (
  id CHAR(36) NOT NULL,
  name VARCHAR(255) NOT NULL,
  time_zone VARCHAR(64) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT schedules_pkey PRIMARY KEY (id),
  CONSTRAINT schedules_name_key UNIQUE (name)
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE TABLE schedule_teams (
  id CHAR(36) NOT NULL,
  schedule_id CHAR(36) NOT NULL,
  team_id CHAR(36) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT schedule_teams_pkey PRIMARY KEY (id),
  CONSTRAINT schedule_teams_schedule_id_team_id_key UNIQUE (schedule_id, team_id),
  CONSTRAINT schedule_teams_schedule_id_fkey FOREIGN KEY (schedule_id) REFERENCES schedules (id) ON DELETE CASCADE
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX schedule_teams_team_id_schedule_id_idx ON schedule_teams (team_id, schedule_id);

CREATE TABLE layers (
  id CHAR(36) NOT NULL,
  schedule_id CHAR(36) NOT NULL,
  position INT NOT NULL,
  name VARCHAR(255) NOT NULL,
  turn_length_seconds BIGINT NOT NULL,
  starts_at DATETIME(6) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT layers_pkey PRIMARY KEY (id),
  CONSTRAINT layers_schedule_id_position_key UNIQUE (schedule_id, position),
  CONSTRAINT layers_schedule_id_fkey FOREIGN KEY (schedule_id) REFERENCES schedules (id) ON DELETE CASCADE
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE TABLE layer_participants (
  id CHAR(36) NOT NULL,
  layer_id CHAR(36) NOT NULL,
  position INT NOT NULL,
  user_id CHAR(36) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT layer_participants_pkey PRIMARY KEY (id),
  CONSTRAINT layer_participants_layer_id_position_key UNIQUE (layer_id, position),
  CONSTRAINT layer_participants_layer_id_fkey FOREIGN KEY (layer_id) REFERENCES layers (id) ON DELETE CASCADE
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX layer_participants_user_id_idx ON layer_participants (user_id);

CREATE TABLE overrides (
  id CHAR(36) NOT NULL,
  schedule_id CHAR(36) NOT NULL,
  user_id CHAR(36) NOT NULL,
  starts_at DATETIME(6) NOT NULL,
  ends_at DATETIME(6) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT overrides_pkey PRIMARY KEY (id),
  CONSTRAINT overrides_schedule_id_fkey FOREIGN KEY (schedule_id) REFERENCES schedules (id) ON DELETE CASCADE,
  CONSTRAINT overrides_ends_at_check CHECK (ends_at > starts_at)
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX overrides_schedule_id_ends_at_idx ON overrides (schedule_id, ends_at);
CREATE INDEX overrides_user_id_idx ON overrides (user_id);
