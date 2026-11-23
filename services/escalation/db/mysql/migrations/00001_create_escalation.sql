-- +goose Up
CREATE TABLE escalation_policies (
  id CHAR(36) NOT NULL,
  name VARCHAR(255) NOT NULL,
  team_id CHAR(36) NOT NULL,
  repeat_count INT NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  CONSTRAINT escalation_policies_pkey PRIMARY KEY (id),
  CONSTRAINT escalation_policies_name_key UNIQUE (name)
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX escalation_policies_team_id_id_idx ON escalation_policies (team_id, id);

CREATE TABLE escalation_levels (
  id CHAR(36) NOT NULL,
  policy_id CHAR(36) NOT NULL,
  position INT NOT NULL,
  delay_seconds BIGINT NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT escalation_levels_pkey PRIMARY KEY (id),
  CONSTRAINT escalation_levels_policy_id_position_key UNIQUE (policy_id, position),
  CONSTRAINT escalation_levels_policy_id_fkey FOREIGN KEY (policy_id) REFERENCES escalation_policies (id) ON DELETE CASCADE
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE TABLE level_targets (
  id CHAR(36) NOT NULL,
  level_id CHAR(36) NOT NULL,
  position INT NOT NULL,
  target_type VARCHAR(16) NOT NULL,
  target_id CHAR(36) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  CONSTRAINT level_targets_pkey PRIMARY KEY (id),
  CONSTRAINT level_targets_level_id_position_key UNIQUE (level_id, position),
  CONSTRAINT level_targets_level_id_fkey FOREIGN KEY (level_id) REFERENCES escalation_levels (id) ON DELETE CASCADE,
  CONSTRAINT level_targets_target_type_check CHECK (target_type IN ('user', 'schedule'))
) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;

CREATE INDEX level_targets_target_type_target_id_idx ON level_targets (target_type, target_id);
