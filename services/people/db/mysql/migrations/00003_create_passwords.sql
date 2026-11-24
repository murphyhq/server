-- +goose Up
CREATE TABLE passwords (
  id CHAR(36) NOT NULL,
  user_id CHAR(36) NOT NULL,
  hash TEXT NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT passwords_pkey PRIMARY KEY (id),
  CONSTRAINT passwords_user_id_key UNIQUE (user_id),
  CONSTRAINT passwords_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

ALTER TABLE one_time_codes DROP CHECK one_time_codes_purpose_check;
ALTER TABLE one_time_codes ADD CONSTRAINT one_time_codes_purpose_check CHECK (purpose IN ('app_sign_in', 'connect', 'set_password'));
