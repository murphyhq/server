-- +goose Up
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

ALTER TABLE one_time_codes DROP CONSTRAINT one_time_codes_purpose_check;
ALTER TABLE one_time_codes ADD CONSTRAINT one_time_codes_purpose_check CHECK (purpose IN ('app_sign_in', 'connect', 'set_password'));
