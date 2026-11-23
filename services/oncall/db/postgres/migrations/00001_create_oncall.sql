-- +goose Up
CREATE TABLE schedules (
  id uuid NOT NULL,
  name text NOT NULL,
  time_zone text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT schedules_pkey PRIMARY KEY (id),
  CONSTRAINT schedules_name_key UNIQUE (name)
);

CREATE TABLE schedule_teams (
  id uuid NOT NULL,
  schedule_id uuid NOT NULL,
  team_id uuid NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT schedule_teams_pkey PRIMARY KEY (id),
  CONSTRAINT schedule_teams_schedule_id_team_id_key UNIQUE (schedule_id, team_id),
  CONSTRAINT schedule_teams_schedule_id_fkey FOREIGN KEY (schedule_id) REFERENCES schedules (id) ON DELETE CASCADE
);

CREATE INDEX schedule_teams_team_id_schedule_id_idx ON schedule_teams (team_id, schedule_id);

CREATE TABLE layers (
  id uuid NOT NULL,
  schedule_id uuid NOT NULL,
  position integer NOT NULL,
  name text NOT NULL,
  turn_length_seconds bigint NOT NULL,
  starts_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT layers_pkey PRIMARY KEY (id),
  CONSTRAINT layers_schedule_id_position_key UNIQUE (schedule_id, position),
  CONSTRAINT layers_schedule_id_fkey FOREIGN KEY (schedule_id) REFERENCES schedules (id) ON DELETE CASCADE
);

CREATE TABLE layer_participants (
  id uuid NOT NULL,
  layer_id uuid NOT NULL,
  position integer NOT NULL,
  user_id uuid NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT layer_participants_pkey PRIMARY KEY (id),
  CONSTRAINT layer_participants_layer_id_position_key UNIQUE (layer_id, position),
  CONSTRAINT layer_participants_layer_id_fkey FOREIGN KEY (layer_id) REFERENCES layers (id) ON DELETE CASCADE
);

CREATE INDEX layer_participants_user_id_idx ON layer_participants (user_id);

CREATE TABLE overrides (
  id uuid NOT NULL,
  schedule_id uuid NOT NULL,
  user_id uuid NOT NULL,
  starts_at timestamptz NOT NULL,
  ends_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT overrides_pkey PRIMARY KEY (id),
  CONSTRAINT overrides_schedule_id_fkey FOREIGN KEY (schedule_id) REFERENCES schedules (id) ON DELETE CASCADE,
  CONSTRAINT overrides_ends_at_check CHECK (ends_at > starts_at)
);

CREATE INDEX overrides_schedule_id_ends_at_idx ON overrides (schedule_id, ends_at);
CREATE INDEX overrides_user_id_idx ON overrides (user_id);
