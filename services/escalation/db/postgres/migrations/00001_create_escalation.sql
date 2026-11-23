-- +goose Up
CREATE TABLE escalation_policies (
  id uuid NOT NULL,
  name text NOT NULL,
  team_id uuid NOT NULL,
  version integer NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  deleted_at timestamptz,
  CONSTRAINT escalation_policies_pkey PRIMARY KEY (id)
);

CREATE UNIQUE INDEX escalation_policies_name_idx ON escalation_policies (name) WHERE deleted_at IS NULL;
CREATE INDEX escalation_policies_team_id_id_idx ON escalation_policies (team_id, id);

CREATE TABLE escalation_policy_versions (
  id uuid NOT NULL,
  policy_id uuid NOT NULL,
  version integer NOT NULL,
  repeat_count integer NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT escalation_policy_versions_pkey PRIMARY KEY (id),
  CONSTRAINT escalation_policy_versions_policy_id_version_key UNIQUE (policy_id, version),
  CONSTRAINT escalation_policy_versions_policy_id_fkey FOREIGN KEY (policy_id) REFERENCES escalation_policies (id) ON DELETE CASCADE
);

CREATE TABLE escalation_levels (
  id uuid NOT NULL,
  version_id uuid NOT NULL,
  position integer NOT NULL,
  delay_seconds bigint NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT escalation_levels_pkey PRIMARY KEY (id),
  CONSTRAINT escalation_levels_version_id_position_key UNIQUE (version_id, position),
  CONSTRAINT escalation_levels_version_id_fkey FOREIGN KEY (version_id) REFERENCES escalation_policy_versions (id) ON DELETE CASCADE
);

CREATE TABLE level_targets (
  id uuid NOT NULL,
  level_id uuid NOT NULL,
  position integer NOT NULL,
  target_type text NOT NULL,
  target_id uuid NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT level_targets_pkey PRIMARY KEY (id),
  CONSTRAINT level_targets_level_id_position_key UNIQUE (level_id, position),
  CONSTRAINT level_targets_level_id_fkey FOREIGN KEY (level_id) REFERENCES escalation_levels (id) ON DELETE CASCADE,
  CONSTRAINT level_targets_target_type_check CHECK (target_type IN ('user', 'schedule'))
);

CREATE INDEX level_targets_target_type_target_id_idx ON level_targets (target_type, target_id);
