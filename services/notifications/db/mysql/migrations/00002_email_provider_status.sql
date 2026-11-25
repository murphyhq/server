-- +goose Up
ALTER TABLE email_provider
  ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'pending',
  ADD COLUMN problem VARCHAR(1024) NOT NULL DEFAULT '',
  ADD COLUMN checked_at DATETIME(6) NULL,
  ADD CONSTRAINT email_provider_status_check CHECK (status IN ('pending', 'ready', 'failing'));

