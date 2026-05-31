CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  agent_id TEXT,
  session_type TEXT NOT NULL,
  csrf_token_hash TEXT NOT NULL,
  expires_at DATETIME NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY(user_id) REFERENCES users(id),
  FOREIGN KEY(agent_id) REFERENCES agents(id)
);

CREATE TABLE IF NOT EXISTS api_tokens (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  token_hash TEXT NOT NULL,
  name TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  revoked_at DATETIME,
  FOREIGN KEY(user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS agents (
  id TEXT PRIMARY KEY,
  linux_username TEXT NOT NULL,
  linux_uid INTEGER NOT NULL,
  socket_path TEXT NOT NULL,
  status TEXT NOT NULL,
  last_seen_at DATETIME,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_tokens (
  id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL,
  token_hash TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  expires_at DATETIME,
  revoked_at DATETIME,
  FOREIGN KEY(agent_id) REFERENCES agents(id)
);

CREATE TABLE IF NOT EXISTS agent_scoped_sessions (
  session_id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL,
  linux_username TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  FOREIGN KEY(session_id) REFERENCES sessions(id),
  FOREIGN KEY(agent_id) REFERENCES agents(id)
);

CREATE TABLE IF NOT EXISTS pods (
  id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL,
  podman_pod_id TEXT NOT NULL,
  name TEXT NOT NULL,
  state TEXT NOT NULL,
  health TEXT,
  created_at DATETIME,
  observed_at DATETIME NOT NULL,
  raw_json TEXT,
  FOREIGN KEY(agent_id) REFERENCES agents(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_pods_agent_podman_id ON pods(agent_id, podman_pod_id);

CREATE TABLE IF NOT EXISTS containers (
  id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL,
  pod_id TEXT,
  podman_container_id TEXT NOT NULL,
  name TEXT NOT NULL,
  image TEXT,
  state TEXT NOT NULL,
  health TEXT,
  created_at DATETIME,
  observed_at DATETIME NOT NULL,
  raw_json TEXT,
  FOREIGN KEY(agent_id) REFERENCES agents(id),
  FOREIGN KEY(pod_id) REFERENCES pods(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_containers_agent_container_id ON containers(agent_id, podman_container_id);

CREATE TABLE IF NOT EXISTS resource_samples (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id TEXT NOT NULL,
  pod_id TEXT,
  container_id TEXT,
  sampled_at DATETIME NOT NULL,
  cpu_podman_raw TEXT,
  cpu_percent_host_total REAL,
  memory_podman_raw TEXT,
  memory_bytes INTEGER,
  raw_json TEXT,
  FOREIGN KEY(agent_id) REFERENCES agents(id),
  FOREIGN KEY(pod_id) REFERENCES pods(id),
  FOREIGN KEY(container_id) REFERENCES containers(id)
);

CREATE INDEX IF NOT EXISTS idx_resource_samples_sampled_at ON resource_samples(sampled_at);

CREATE TABLE IF NOT EXISTS security_scans (
  id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL,
  status TEXT NOT NULL,
  scanner TEXT NOT NULL,
  scanner_version TEXT,
  started_at DATETIME NOT NULL,
  finished_at DATETIME,
  summary_json TEXT,
  error_code TEXT,
  error_message TEXT,
  FOREIGN KEY(agent_id) REFERENCES agents(id)
);

CREATE TABLE IF NOT EXISTS security_findings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  scan_id TEXT NOT NULL,
  image_digest TEXT,
  target TEXT NOT NULL,
  vulnerability_id TEXT NOT NULL,
  severity TEXT NOT NULL,
  title TEXT,
  package_name TEXT,
  installed_version TEXT,
  fixed_version TEXT,
  raw_json TEXT,
  FOREIGN KEY(scan_id) REFERENCES security_scans(id)
);

CREATE TABLE IF NOT EXISTS image_digests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id TEXT NOT NULL,
  image_name TEXT NOT NULL,
  local_digest TEXT,
  remote_digest TEXT,
  update_available INTEGER NOT NULL DEFAULT 0,
  checked_at DATETIME NOT NULL,
  error_message TEXT,
  FOREIGN KEY(agent_id) REFERENCES agents(id)
);

CREATE TABLE IF NOT EXISTS host_package_updates (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id TEXT NOT NULL,
  package_name TEXT NOT NULL,
  installed_version TEXT,
  available_version TEXT,
  update_available INTEGER NOT NULL DEFAULT 0,
  checked_at DATETIME NOT NULL,
  raw_json TEXT,
  FOREIGN KEY(agent_id) REFERENCES agents(id)
);

CREATE TABLE IF NOT EXISTS audit_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at DATETIME NOT NULL,
  actor_user_id TEXT,
  agent_id TEXT,
  action TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT,
  result TEXT NOT NULL,
  correlation_id TEXT NOT NULL,
  details_json TEXT,
  FOREIGN KEY(actor_user_id) REFERENCES users(id),
  FOREIGN KEY(agent_id) REFERENCES agents(id)
);

CREATE INDEX IF NOT EXISTS idx_audit_events_created_at ON audit_events(created_at);
CREATE INDEX IF NOT EXISTS idx_audit_events_correlation_id ON audit_events(correlation_id);

CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS log_sources (
  id TEXT PRIMARY KEY,
  agent_id TEXT,
  source_type TEXT NOT NULL,
  pod_id TEXT,
  container_id TEXT,
  name TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  FOREIGN KEY(agent_id) REFERENCES agents(id),
  FOREIGN KEY(pod_id) REFERENCES pods(id),
  FOREIGN KEY(container_id) REFERENCES containers(id)
);

CREATE TABLE IF NOT EXISTS log_files (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id TEXT NOT NULL,
  path TEXT NOT NULL,
  started_at DATETIME NOT NULL,
  ended_at DATETIME,
  compressed INTEGER NOT NULL DEFAULT 0,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  sha256 TEXT,
  FOREIGN KEY(source_id) REFERENCES log_sources(id)
);

CREATE TABLE IF NOT EXISTS log_offsets (
  source_id TEXT PRIMARY KEY,
  file_id INTEGER,
  byte_offset INTEGER NOT NULL DEFAULT 0,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY(source_id) REFERENCES log_sources(id),
  FOREIGN KEY(file_id) REFERENCES log_files(id)
);

CREATE TABLE IF NOT EXISTS pod_templates (
  id TEXT PRIMARY KEY,
  version TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT NOT NULL,
  image TEXT NOT NULL,
  template_json TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS created_pods (
  id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL,
  pod_id TEXT,
  source_type TEXT NOT NULL,
  source_id TEXT,
  preview_command_json TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  FOREIGN KEY(agent_id) REFERENCES agents(id),
  FOREIGN KEY(pod_id) REFERENCES pods(id)
);

CREATE TABLE IF NOT EXISTS image_builds (
  id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL,
  image_name TEXT NOT NULL,
  dockerfile_hash TEXT NOT NULL,
  status TEXT NOT NULL,
  started_at DATETIME NOT NULL,
  finished_at DATETIME,
  metadata_json TEXT,
  FOREIGN KEY(agent_id) REFERENCES agents(id)
);

CREATE TABLE IF NOT EXISTS secrets_metadata (
  id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL,
  secret_name TEXT NOT NULL,
  fingerprint TEXT,
  used_by_pod_id TEXT,
  used_by_container_id TEXT,
  created_at DATETIME NOT NULL,
  FOREIGN KEY(agent_id) REFERENCES agents(id),
  FOREIGN KEY(used_by_pod_id) REFERENCES pods(id),
  FOREIGN KEY(used_by_container_id) REFERENCES containers(id)
);

CREATE TABLE IF NOT EXISTS automation_rules (
  id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL,
  name TEXT NOT NULL,
  rule_type TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 0,
  dry_run INTEGER NOT NULL DEFAULT 1,
  cooldown_seconds INTEGER NOT NULL,
  max_actions_per_hour INTEGER NOT NULL,
  config_json TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY(agent_id) REFERENCES agents(id)
);

CREATE TABLE IF NOT EXISTS automation_runs (
  id TEXT PRIMARY KEY,
  rule_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  action TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT,
  result TEXT NOT NULL,
  dry_run INTEGER NOT NULL,
  created_at DATETIME NOT NULL,
  FOREIGN KEY(rule_id) REFERENCES automation_rules(id),
  FOREIGN KEY(agent_id) REFERENCES agents(id)
);

CREATE TABLE IF NOT EXISTS debug_traces (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at DATETIME NOT NULL,
  mode TEXT NOT NULL,
  component TEXT NOT NULL,
  operation TEXT NOT NULL,
  correlation_id TEXT NOT NULL,
  agent_id TEXT,
  target_type TEXT,
  target_id TEXT,
  trace_json TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_debug_traces_correlation_id ON debug_traces(correlation_id);

