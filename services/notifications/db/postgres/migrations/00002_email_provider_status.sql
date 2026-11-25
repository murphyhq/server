-- +goose Up
ALTER TABLE email_provider
  ADD COLUMN status text NOT NULL DEFAULT 'pending',
  ADD COLUMN problem text NOT NULL DEFAULT '',
  ADD COLUMN checked_at timestamptz,
  ADD CONSTRAINT email_provider_status_check CHECK (status IN ('pending', 'ready', 'failing'));

