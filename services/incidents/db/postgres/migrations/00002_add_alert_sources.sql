-- +goose Up
ALTER TABLE alerts
  ADD COLUMN source text NOT NULL DEFAULT 'generic',
  ADD COLUMN instance text,
  ADD COLUMN annotations jsonb NOT NULL DEFAULT '{}';
