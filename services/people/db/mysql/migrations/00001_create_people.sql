-- +goose Up
CREATE TABLE users (
  id CHAR(36) NOT NULL,
  name VARCHAR(255) NOT NULL,
  email VARCHAR(320) NOT NULL,
  time_zone VARCHAR(64) NOT NULL,
  status VARCHAR(16) NOT NULL,
  role VARCHAR(16) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT users_pkey PRIMARY KEY (id),
  CONSTRAINT users_email_key UNIQUE (email),
  CONSTRAINT users_status_check CHECK (status IN ('active', 'deactivated')),
  CONSTRAINT users_role_check CHECK (role IN ('admin', 'member'))
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

CREATE TABLE identities (
  id CHAR(36) NOT NULL,
  user_id CHAR(36) NOT NULL,
  issuer VARCHAR(512) NOT NULL,
  subject VARCHAR(255) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT identities_pkey PRIMARY KEY (id),
  CONSTRAINT identities_issuer_subject_key UNIQUE (issuer, subject),
  CONSTRAINT identities_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX identities_user_id_idx ON identities (user_id);

CREATE TABLE sessions (
  id CHAR(36) NOT NULL,
  user_id CHAR(36) NOT NULL,
  token_hash VARBINARY(32) NOT NULL,
  expires_at DATETIME(6) NOT NULL,
  revoked_at DATETIME(6),
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT sessions_pkey PRIMARY KEY (id),
  CONSTRAINT sessions_token_hash_key UNIQUE (token_hash),
  CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX sessions_user_id_idx ON sessions (user_id);

CREATE TABLE sign_ins (
  id CHAR(36) NOT NULL,
  state_hash VARBINARY(32) NOT NULL,
  nonce VARCHAR(64) NOT NULL,
  verifier VARCHAR(128) NOT NULL,
  return_to VARCHAR(2048),
  code_challenge VARCHAR(128),
  expires_at DATETIME(6) NOT NULL,
  used_at DATETIME(6),
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT sign_ins_pkey PRIMARY KEY (id),
  CONSTRAINT sign_ins_state_hash_key UNIQUE (state_hash)
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX sign_ins_expires_at_idx ON sign_ins (expires_at);

CREATE TABLE one_time_codes (
  id CHAR(36) NOT NULL,
  code_hash VARBINARY(32) NOT NULL,
  user_id CHAR(36) NOT NULL,
  purpose VARCHAR(32) NOT NULL,
  code_challenge VARCHAR(128),
  expires_at DATETIME(6) NOT NULL,
  used_at DATETIME(6),
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT one_time_codes_pkey PRIMARY KEY (id),
  CONSTRAINT one_time_codes_code_hash_key UNIQUE (code_hash),
  CONSTRAINT one_time_codes_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT one_time_codes_purpose_check CHECK (purpose IN ('app_sign_in', 'connect'))
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX one_time_codes_user_id_idx ON one_time_codes (user_id);
