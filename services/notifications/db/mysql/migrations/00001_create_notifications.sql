-- +goose Up
CREATE TABLE pages (
  id CHAR(36) NOT NULL,
  incident_id CHAR(36) NOT NULL,
  escalation_level INT NOT NULL,
  escalation_round INT NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT pages_pkey PRIMARY KEY (id)
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX pages_incident_id_idx ON pages (incident_id);

CREATE TABLE page_recipients (
  id CHAR(36) NOT NULL,
  page_id CHAR(36) NOT NULL,
  incident_id CHAR(36) NOT NULL,
  user_id CHAR(36) NOT NULL,
  via_type VARCHAR(32) NOT NULL,
  via_id CHAR(36) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT page_recipients_pkey PRIMARY KEY (id),
  CONSTRAINT page_recipients_page_id_fkey FOREIGN KEY (page_id) REFERENCES pages (id) ON DELETE CASCADE,
  CONSTRAINT page_recipients_page_id_user_id_key UNIQUE (page_id, user_id),
  CONSTRAINT page_recipients_via_type_check CHECK (via_type IN ('user', 'schedule'))
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX page_recipients_incident_id_user_id_idx ON page_recipients (incident_id, user_id);

CREATE TABLE notifications (
  id CHAR(36) NOT NULL,
  subject VARCHAR(16) NOT NULL,
  subject_id CHAR(36) NOT NULL,
  page_id CHAR(36),
  user_id CHAR(36) NOT NULL,
  contact_method_id CHAR(36) NOT NULL,
  channel VARCHAR(32) NOT NULL,
  reason VARCHAR(32) NOT NULL,
  status VARCHAR(32) NOT NULL,
  failure TEXT,
  sent_at DATETIME(6),
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT notifications_pkey PRIMARY KEY (id),
  CONSTRAINT notifications_page_id_fkey FOREIGN KEY (page_id) REFERENCES pages (id) ON DELETE CASCADE,
  CONSTRAINT notifications_channel_check CHECK (channel IN ('email', 'sms', 'voice', 'webhook', 'push')),
  CONSTRAINT notifications_reason_check CHECK (reason IN ('paged', 'acknowledged', 'resolved', 'test')),
  CONSTRAINT notifications_subject_check CHECK (subject IN ('incident', 'test')),
  CONSTRAINT notifications_status_check CHECK (status IN ('created', 'sent', 'failed', 'cancelled'))
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX notifications_subject_subject_id_idx ON notifications (subject, subject_id);

CREATE TABLE handled_messages (
  id CHAR(36) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT handled_messages_pkey PRIMARY KEY (id)
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX handled_messages_created_at_idx ON handled_messages (created_at);

CREATE TABLE scheduled_messages (
  id CHAR(36) NOT NULL,
  due_at DATETIME(6) NOT NULL,
  claimed_until DATETIME(6),
  envelope LONGBLOB NOT NULL,
  created_at DATETIME(6) NOT NULL,
  waiting_incident_id CHAR(36) NULL,
  waiting_notification_id CHAR(36) NULL,
  CONSTRAINT scheduled_messages_pkey PRIMARY KEY (id)
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX scheduled_messages_waiting_incident_id_idx ON scheduled_messages (waiting_incident_id);

CREATE INDEX scheduled_messages_due_at_id_idx ON scheduled_messages (due_at, id);

CREATE TABLE email_provider (
  singleton BOOLEAN NOT NULL DEFAULT TRUE,
  type VARCHAR(16) NOT NULL,
  from_address VARCHAR(320) NOT NULL,
  host VARCHAR(255) NOT NULL,
  port INT NOT NULL,
  username VARCHAR(255) NOT NULL,
  secret VARCHAR(1024) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'pending',
  problem VARCHAR(1024) NOT NULL DEFAULT '',
  checked_at DATETIME(6) NULL,
  CONSTRAINT email_provider_pkey PRIMARY KEY (singleton),
  CONSTRAINT email_provider_singleton_check CHECK (singleton = TRUE),
  CONSTRAINT email_provider_type_check CHECK (type IN ('smtp')),
  CONSTRAINT email_provider_status_check CHECK (status IN ('pending', 'ready', 'failing'))
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
