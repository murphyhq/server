-- +goose Up
CREATE TABLE users (
  id uuid NOT NULL,
  name text NOT NULL,
  email text NOT NULL,
  time_zone text NOT NULL,
  status text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT users_pkey PRIMARY KEY (id),
  CONSTRAINT users_email_key UNIQUE (email),
  CONSTRAINT users_status_check CHECK (status IN ('active', 'deactivated'))
);

CREATE TABLE contact_methods (
  id uuid NOT NULL,
  user_id uuid NOT NULL,
  type text NOT NULL,
  address text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT contact_methods_pkey PRIMARY KEY (id),
  CONSTRAINT contact_methods_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT contact_methods_type_check CHECK (type IN ('email', 'sms', 'voice', 'webhook', 'push'))
);

CREATE INDEX contact_methods_user_id_id_idx ON contact_methods (user_id, id);

CREATE TABLE teams (
  id uuid NOT NULL,
  name text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT teams_pkey PRIMARY KEY (id),
  CONSTRAINT teams_name_key UNIQUE (name)
);

CREATE TABLE team_memberships (
  id uuid NOT NULL,
  team_id uuid NOT NULL,
  user_id uuid NOT NULL,
  role text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT team_memberships_pkey PRIMARY KEY (id),
  CONSTRAINT team_memberships_team_id_user_id_key UNIQUE (team_id, user_id),
  CONSTRAINT team_memberships_team_id_fkey FOREIGN KEY (team_id) REFERENCES teams (id) ON DELETE CASCADE,
  CONSTRAINT team_memberships_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT team_memberships_role_check CHECK (role IN ('manager', 'responder', 'observer'))
);

CREATE INDEX team_memberships_user_id_idx ON team_memberships (user_id);
