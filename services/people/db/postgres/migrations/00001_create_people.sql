-- +goose Up
CREATE TABLE users (
  id uuid NOT NULL,
  name text NOT NULL,
  email text NOT NULL,
  time_zone text NOT NULL,
  status text NOT NULL,
  role text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT users_pkey PRIMARY KEY (id),
  CONSTRAINT users_email_key UNIQUE (email),
  CONSTRAINT users_status_check CHECK (status IN ('active', 'deactivated')),
  CONSTRAINT users_role_check CHECK (role IN ('admin', 'member'))
);

CREATE TABLE contact_methods (
  id uuid NOT NULL,
  user_id uuid NOT NULL,
  type text NOT NULL,
  address text NOT NULL,
  push_platform text,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  disabled_at timestamptz,
  disabled_reason text,
  CONSTRAINT contact_methods_pkey PRIMARY KEY (id),
  CONSTRAINT contact_methods_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT contact_methods_type_check CHECK (type IN ('email', 'sms', 'voice', 'webhook', 'push')),
  CONSTRAINT contact_methods_push_platform_check CHECK ((type = 'push' AND COALESCE(push_platform, '') IN ('ios', 'android')) OR (type <> 'push' AND push_platform IS NULL))
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

CREATE TABLE identities (
  id uuid NOT NULL,
  user_id uuid NOT NULL,
  issuer text NOT NULL,
  subject text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT identities_pkey PRIMARY KEY (id),
  CONSTRAINT identities_issuer_subject_key UNIQUE (issuer, subject),
  CONSTRAINT identities_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX identities_user_id_idx ON identities (user_id);

CREATE TABLE sessions (
  id uuid NOT NULL,
  user_id uuid NOT NULL,
  token_hash bytea NOT NULL,
  client text NOT NULL,
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT sessions_pkey PRIMARY KEY (id),
  CONSTRAINT sessions_token_hash_key UNIQUE (token_hash),
  CONSTRAINT sessions_client_check CHECK (client IN ('browser', 'app', 'token')),
  CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);

CREATE TABLE sign_ins (
  id uuid NOT NULL,
  state_hash bytea NOT NULL,
  nonce text NOT NULL,
  verifier text NOT NULL,
  return_to text,
  code_challenge text,
  expires_at timestamptz NOT NULL,
  used_at timestamptz,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT sign_ins_pkey PRIMARY KEY (id),
  CONSTRAINT sign_ins_state_hash_key UNIQUE (state_hash)
);

CREATE INDEX sign_ins_expires_at_idx ON sign_ins (expires_at);

CREATE TABLE one_time_codes (
  id uuid NOT NULL,
  code_hash bytea NOT NULL,
  user_id uuid NOT NULL,
  purpose text NOT NULL,
  code_challenge text,
  expires_at timestamptz NOT NULL,
  used_at timestamptz,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT one_time_codes_pkey PRIMARY KEY (id),
  CONSTRAINT one_time_codes_code_hash_key UNIQUE (code_hash),
  CONSTRAINT one_time_codes_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT one_time_codes_purpose_check CHECK (purpose IN ('app_sign_in', 'connect', 'set_password'))
);

CREATE INDEX one_time_codes_user_id_idx ON one_time_codes (user_id);

CREATE TABLE passwords (
  id uuid NOT NULL,
  user_id uuid NOT NULL,
  hash text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT passwords_pkey PRIMARY KEY (id),
  CONSTRAINT passwords_user_id_key UNIQUE (user_id),
  CONSTRAINT passwords_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);
