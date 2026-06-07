export interface ApiEnvelope<T> {
  ok: boolean;
  data: T | null;
  error: ApiEnvelopeError | null;
  correlation_id: string;
}

export interface ApiEnvelopeError {
  code: string;
  message: string;
  details: Record<string, unknown>;
}

export interface Agent {
  id: string;
  linux_username: string;
  linux_uid: number;
  socket_path: string;
  status: string;
  last_seen_at?: string;
  created_at: string;
  updated_at: string;
}

export interface CreatedAgentToken {
  agent: Agent;
  token: string;
}

export interface PasskeyCredential {
  id: string;
  user_id: string;
  credential_id: string;
  name: string;
  created_at: string;
  updated_at: string;
  last_used_at?: string | null;
}

export interface PasskeyBeginResponse {
  flow_id: string;
  public_key: Record<string, unknown>;
}

export interface Pod {
  id: string;
  agent_id: string;
  podman_pod_id: string;
  name: string;
  state: string;
  health: string;
  created_at: string;
  observed_at: string;
  raw_json: string;
}

export interface Container {
  id: string;
  agent_id: string;
  pod_id: string;
  podman_container_id: string;
  name: string;
  image: string;
  state: string;
  health: string;
  created_at: string;
  observed_at: string;
  raw_json: string;
}

export interface ResourceSample {
  id: number;
  agent_id: string;
  pod_id: string;
  container_id: string;
  sampled_at: string;
  cpu_podman_raw: string;
  cpu_percent_host_total: number;
  memory_podman_raw: string;
  memory_bytes: number;
  raw_json: string;
}

export interface PodView extends Pod {
  containers: Container[];
  stats: ResourceSample[];
  self_management: boolean;
  snapshot_source?: string;
}

export interface PodDetail {
  pod: Pod;
  containers: Container[];
}

export interface AuditEvent {
  id: number;
  created_at: string;
  actor_user_id: string;
  agent_id: string;
  action: string;
  target_type: string;
  target_id: string;
  result: string;
  correlation_id: string;
  details: Record<string, unknown>;
}

export interface SecurityScan {
  id: string;
  agent_id: string;
  status: string;
  scanner: string;
  scanner_version: string;
  started_at: string;
  finished_at?: string;
  summary: Record<string, unknown>;
  error_code?: string;
  error_message?: string;
}

export interface SecurityFinding {
  id: number;
  scan_id: string;
  image_digest: string;
  target: string;
  vulnerability_id: string;
  severity: string;
  title: string;
  package_name: string;
  installed_version: string;
  fixed_version: string;
  raw_json: string;
}

export interface ImageDigest {
  id: number;
  agent_id: string;
  image_name: string;
  local_digest: string;
  remote_digest: string;
  update_available: boolean;
  checked_at: string;
  error_message: string;
}

export interface HostPackageUpdate {
  id: number;
  agent_id: string;
  package_name: string;
  installed_version: string;
  available_version: string;
  update_available: boolean;
  checked_at: string;
  raw_json: string;
}

export interface SecuritySummary {
  status: string;
  latest_scan: SecurityScan | null;
  scanner: string;
  scanner_available?: boolean;
  scanner_error?: string;
  scheduled_scans: boolean;
  image_digest: string;
  host_packages: string;
  image_digests?: ImageDigest[];
  host_updates?: HostPackageUpdate[];
}

export interface ScannerInstallOption {
  id: string;
  title: string;
  description: string;
  command: string;
  available: boolean;
  requires_sudo: boolean;
  official: boolean;
  docs_url: string;
}

export interface ScannerOptions {
  scanner: string;
  scanner_available: boolean;
  scanner_path: string;
  scanner_version?: string;
  scanner_error: string;
  options: ScannerInstallOption[];
}

export interface LogLine {
  timestamp: string;
  source: string;
  line: string;
}

export interface LogHistory {
  lines: LogLine[];
  source?: string;
  since: string;
}

export interface PodTemplate {
  id: string;
  version: string;
  name: string;
  description: string;
  image: string;
  command: string[];
  ports: Array<{ host: number; container: number; protocol: string }>;
  volumes: Array<{ host_path: string; container_path: string; read_only: boolean }>;
  environment: Record<string, string>;
  secrets: Array<{ name: string; target: string; required: boolean; description: string }>;
  health_command: string[];
  resource_limits: { cpu: string; memory: string };
  restart_policy: string;
  labels: Record<string, string>;
  ui_notes: string[];
  custom?: boolean;
}

export interface ComposeStackService {
  name: string;
  image?: string;
  build?: string;
  container_name?: string;
  restart?: string;
  ports?: string[];
  profiles?: string[];
}

export interface ComposeStack {
  id: string;
  version: string;
  name: string;
  description: string;
  source_path: string;
  compose_files: string[];
  services: ComposeStackService[];
  environment_files: string[];
  required_files: string[];
  notes: string[];
  labels: Record<string, string>;
}

export interface AppSettings {
  mode?: string;
  database?: { path: string };
  ui?: { dist_path: string };
  server?: {
    listen_addr: string;
    public_url: string;
    trusted_proxy_mode: boolean;
    tls_cert_file?: string;
    tls_key_file?: string;
    tls_ca_file?: string;
  };
  auth?: {
    session_ttl: number;
    failed_login_limit: number;
    failed_login_window: number;
  };
  metrics?: {
    live_interval: number;
    persist_interval: number;
    retention: number;
  };
  logs?: {
    retention: number;
    per_pod_limit_mb: number;
    total_limit_mb: number;
  };
  security?: {
    scanner: string;
    scheduled_scans_enabled: boolean;
    schedule: string;
  };
  actions?: {
    exec_enabled: boolean;
    automation_enabled: boolean;
  };
}

export interface RuntimeMode {
  mode: string;
  raw_traces_available: boolean;
  production_safe_summary: boolean;
}

export interface SystemStatus {
  runtime_mode: string;
  public_url: string;
  active_backend_port: string;
  ui_build_timestamp: string;
  primary_agent_health: Record<string, unknown>;
  podman_availability: Record<string, unknown>;
  fallback_mode: string;
  dev_supervisor?: Record<string, unknown>;
}

export interface DebugTrace {
  id: number;
  created_at: string;
  mode: string;
  component: string;
  operation: string;
  correlation_id: string;
  agent_id: string;
  target_type: string;
  target_id: string;
  trace: Record<string, unknown>;
}

export interface CurrentUser {
  id?: string;
  username?: string;
  session_type: string;
  agent_id?: string;
  password_change_required?: boolean;
  password_change_reasons?: string[];
  using_configured_password?: boolean;
  passkeys_registered?: number;
  [key: string]: unknown;
}

export type LifecycleAction = 'start' | 'stop' | 'restart' | 'kill';
