-- +goose Up
CREATE TABLE escalation_policies (
  id CHAR(36) NOT NULL,
  name VARCHAR(255) NOT NULL,
  team_id CHAR(36) NOT NULL,
  version INT NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  deleted_at DATETIME(6),
  active_name VARCHAR(255) AS (CASE WHEN deleted_at IS NULL THEN name END) STORED,
  CONSTRAINT escalation_policies_pkey PRIMARY KEY (id),
  CONSTRAINT escalation_policies_name_idx UNIQUE (active_name)
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX escalation_policies_team_id_id_idx ON escalation_policies (team_id, id);

CREATE TABLE escalation_policy_versions (
  id CHAR(36) NOT NULL,
  policy_id CHAR(36) NOT NULL,
  version INT NOT NULL,
  repeat_count INT NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT escalation_policy_versions_pkey PRIMARY KEY (id),
  CONSTRAINT escalation_policy_versions_policy_id_version_key UNIQUE (policy_id, version),
  CONSTRAINT escalation_policy_versions_policy_id_fkey FOREIGN KEY (policy_id) REFERENCES escalation_policies (id) ON DELETE CASCADE
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE TABLE escalation_levels (
  id CHAR(36) NOT NULL,
  version_id CHAR(36) NOT NULL,
  position INT NOT NULL,
  delay_seconds BIGINT NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT escalation_levels_pkey PRIMARY KEY (id),
  CONSTRAINT escalation_levels_version_id_position_key UNIQUE (version_id, position),
  CONSTRAINT escalation_levels_version_id_fkey FOREIGN KEY (version_id) REFERENCES escalation_policy_versions (id) ON DELETE CASCADE
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE TABLE level_targets (
  id CHAR(36) NOT NULL,
  level_id CHAR(36) NOT NULL,
  position INT NOT NULL,
  target_type VARCHAR(32) NOT NULL,
  target_id CHAR(36) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT level_targets_pkey PRIMARY KEY (id),
  CONSTRAINT level_targets_level_id_position_key UNIQUE (level_id, position),
  CONSTRAINT level_targets_level_id_fkey FOREIGN KEY (level_id) REFERENCES escalation_levels (id) ON DELETE CASCADE,
  CONSTRAINT level_targets_target_type_check CHECK (target_type IN ('user', 'schedule'))
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX level_targets_target_type_target_id_idx ON level_targets (target_type, target_id);
