-- +goose Up
ALTER TABLE contact_methods ADD COLUMN disabled_at timestamptz;
ALTER TABLE contact_methods ADD COLUMN disabled_reason text;
