-- +goose Up
ALTER TABLE contact_methods ADD COLUMN disabled_at DATETIME(6) NULL;
ALTER TABLE contact_methods ADD COLUMN disabled_reason TEXT NULL;
