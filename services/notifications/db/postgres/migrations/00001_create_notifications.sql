-- +goose Up
CREATE TABLE pages (
  id uuid NOT NULL,
  incident_id uuid NOT NULL,
  escalation_level integer NOT NULL,
  escalation_round integer NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT pages_pkey PRIMARY KEY (id)
);

CREATE INDEX pages_incident_id_idx ON pages (incident_id);

CREATE TABLE page_recipients (
  id uuid NOT NULL,
  page_id uuid NOT NULL,
  incident_id uuid NOT NULL,
  user_id uuid NOT NULL,
  via_type text NOT NULL,
  via_id uuid NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT page_recipients_pkey PRIMARY KEY (id),
  CONSTRAINT page_recipients_page_id_fkey FOREIGN KEY (page_id) REFERENCES pages (id) ON DELETE CASCADE,
  CONSTRAINT page_recipients_page_id_user_id_key UNIQUE (page_id, user_id),
  CONSTRAINT page_recipients_via_type_check CHECK (via_type IN ('user', 'schedule'))
);

CREATE INDEX page_recipients_incident_id_user_id_idx ON page_recipients (incident_id, user_id);

CREATE TABLE notifications (
  id uuid NOT NULL,
  incident_id uuid NOT NULL,
  page_id uuid,
  user_id uuid NOT NULL,
  contact_method_id uuid NOT NULL,
  channel text NOT NULL,
  reason text NOT NULL,
  status text NOT NULL,
  failure text,
  sent_at timestamptz,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT notifications_pkey PRIMARY KEY (id),
  CONSTRAINT notifications_page_id_fkey FOREIGN KEY (page_id) REFERENCES pages (id) ON DELETE CASCADE,
  CONSTRAINT notifications_channel_check CHECK (channel IN ('email', 'sms', 'voice', 'webhook', 'push')),
  CONSTRAINT notifications_reason_check CHECK (reason IN ('paged', 'acknowledged', 'resolved')),
  CONSTRAINT notifications_status_check CHECK (status IN ('created', 'sent', 'failed'))
);

CREATE INDEX notifications_incident_id_idx ON notifications (incident_id);

CREATE TABLE handled_messages (
  id uuid NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT handled_messages_pkey PRIMARY KEY (id)
);

CREATE INDEX handled_messages_created_at_idx ON handled_messages (created_at);

CREATE TABLE scheduled_messages (
  id uuid NOT NULL,
  due_at timestamptz NOT NULL,
  claimed_until timestamptz,
  envelope bytea NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT scheduled_messages_pkey PRIMARY KEY (id)
);

CREATE INDEX scheduled_messages_due_at_id_idx ON scheduled_messages (due_at, id);
