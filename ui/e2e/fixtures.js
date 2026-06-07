const now = '2026-06-06T10:00:00.000Z';
const defaultAdminPassword = 'podorel-development-password';

const primaryAgent = {
  id: 'primary',
  linux_username: 'curly',
  linux_uid: 1000,
  socket_path: '/tmp/podorel-ui-e2e/run/podorel-agent.sock',
  status: 'online',
  last_seen_at: now,
  created_at: now,
  updated_at: now
};

const extraAgent = {
  id: 'e2e-extra',
  linux_username: 'alice',
  linux_uid: 1001,
  socket_path: '/tmp/podorel-ui-e2e/run/e2e-extra.sock',
  status: 'offline',
  last_seen_at: '',
  created_at: now,
  updated_at: now
};

const runningContainer = {
  id: 'ctr-e2e',
  agent_id: 'primary',
  pod_id: 'pod-e2e',
  podman_container_id: 'podman-ctr-e2e',
  name: 'e2e-web-main',
  image: 'docker.io/library/nginx:1.27-alpine',
  state: 'running',
  health: 'healthy',
  created_at: '2026-06-06T08:00:00.000Z',
  observed_at: now,
  raw_json: JSON.stringify({ Status: 'running', Health: 'healthy', RestartCount: 0 })
};

const exitedContainer = {
  id: 'ctr-e2e-worker',
  agent_id: 'primary',
  pod_id: 'pod-e2e',
  podman_container_id: 'podman-ctr-e2e-worker',
  name: 'e2e-worker',
  image: 'docker.io/library/busybox:1.36',
  state: 'exited',
  health: 'unknown',
  created_at: '2026-06-06T08:00:00.000Z',
  observed_at: now,
  raw_json: JSON.stringify({ Status: 'exited', ExitCode: 0 })
};

const runningPod = {
  id: 'pod-e2e',
  agent_id: 'primary',
  podman_pod_id: 'podman-pod-e2e',
  name: 'e2e-web',
  state: 'running',
  health: 'healthy',
  created_at: '2026-06-06T08:00:00.000Z',
  observed_at: now,
  raw_json: JSON.stringify({ State: 'Running' }),
  containers: [runningContainer, exitedContainer],
  stats: [
    {
      id: 11,
      agent_id: 'primary',
      pod_id: 'pod-e2e',
      container_id: 'ctr-e2e',
      sampled_at: now,
      cpu_podman_raw: '1.5%',
      cpu_percent_host_total: 1.5,
      memory_podman_raw: '22MiB / 256MiB',
      memory_bytes: 23_068_672,
      raw_json: JSON.stringify({ MemUsage: '22MiB / 256MiB' })
    }
  ],
  self_management: false,
  snapshot_source: 'fixture'
};

const logLines = [
  { timestamp: now, source: 'e2e-web-main', line: 'ready: nginx started successfully' },
  { timestamp: now, source: 'e2e-worker', line: 'warning: worker exited cleanly' },
  { timestamp: now, source: 'agent', line: 'ok: health check complete' }
];

const auditEvents = [
  {
    id: 91,
    created_at: now,
    actor_user_id: 'admin',
    agent_id: 'primary',
    action: 'pods.restart',
    target_type: 'pod',
    target_id: 'pod-e2e',
    result: 'success',
    correlation_id: 'corr-e2e-pod',
    details: { source: 'e2e' }
  },
  {
    id: 92,
    created_at: now,
    actor_user_id: 'admin',
    agent_id: 'primary',
    action: 'containers.stop',
    target_type: 'container',
    target_id: 'ctr-e2e',
    result: 'success',
    correlation_id: 'corr-e2e-container',
    details: { source: 'e2e' }
  }
];

const scannerOptions = {
  scanner: 'trivy',
  scanner_available: true,
  scanner_path: '/usr/bin/trivy',
  scanner_version: 'Version: 0.70.0',
  scanner_error: '',
  options: [
    {
      id: 'custom-scanner-path',
      title: 'Use an existing scanner path',
      description: 'Existing safe scanner path.',
      command: '/usr/bin/trivy --version',
      available: true,
      requires_sudo: false,
      official: false,
      docs_url: 'https://trivy.dev/docs/latest/getting-started/installation/'
    }
  ]
};

const latestScan = {
  id: 'scan-e2e',
  agent_id: 'primary',
  status: 'complete',
  scanner: 'trivy',
  scanner_version: 'Version: 0.70.0',
  started_at: now,
  finished_at: now,
  summary: { critical: 0, high: 1, medium: 2, low: 3, unknown: 0 }
};

const securitySummary = {
  status: 'complete',
  latest_scan: latestScan,
  scanner: 'trivy',
  scanner_available: true,
  scanner_error: '',
  scheduled_scans: true,
  image_digest: '1 update available',
  host_packages: '1 update available'
};

const finding = {
  id: 7,
  scan_id: 'scan-e2e',
  image_digest: 'sha256:e2e',
  target: 'docker.io/library/nginx:1.27-alpine',
  vulnerability_id: 'CVE-2099-E2E',
  severity: 'HIGH',
  title: 'Fixture vulnerability',
  package_name: 'openssl',
  installed_version: '1.0.0',
  fixed_version: '1.0.1',
  raw_json: '{}'
};

const imageDigest = {
  id: 3,
  agent_id: 'primary',
  image_name: 'docker.io/library/nginx:1.27-alpine',
  local_digest: 'sha256:local',
  remote_digest: 'sha256:remote',
  update_available: true,
  checked_at: now,
  error_message: ''
};

const hostUpdate = {
  id: 4,
  agent_id: 'primary',
  package_name: 'podman',
  installed_version: '5.0.0',
  available_version: '5.1.0',
  update_available: true,
  checked_at: now,
  raw_json: '{}'
};

const settings = {
  mode: 'development',
  database: { path: '/tmp/podorel-ui-e2e/data/podorel.db' },
  ui: { dist_path: 'ui/dist/podorel-ui/browser' },
  server: {
    listen_addr: 'localhost:18080',
    public_url: 'http://localhost:18080',
    trusted_proxy_mode: false
  },
  auth: {
    session_ttl: 86_400_000_000_000,
    failed_login_limit: 5,
    failed_login_window: 900_000_000_000
  },
  metrics: {
    live_interval: 2_000_000_000,
    persist_interval: 30_000_000_000,
    retention: 604_800_000_000_000
  },
  logs: {
    retention: 604_800_000_000_000,
    per_pod_limit_mb: 100,
    total_limit_mb: 5120
  },
  security: {
    scanner: 'trivy',
    scheduled_scans_enabled: true,
    schedule: 'daily'
  },
  actions: {
    exec_enabled: false,
    automation_enabled: false
  }
};

const redisTemplate = {
  id: 'redis-cache',
  version: '1.0.0',
  name: 'Redis Cache',
  description: 'Single-node Redis cache with append-only persistence.',
  image: 'docker.io/library/redis:7-alpine',
  command: ['redis-server', '--appendonly', 'yes', '--appendfsync', 'everysec'],
  ports: [{ host: 6379, container: 6379, protocol: 'tcp' }],
  volumes: [{ host_path: 'redis-data', container_path: '/data', read_only: false }],
  environment: {},
  secrets: [],
  health_command: ['redis-cli', 'ping'],
  resource_limits: { cpu: '0.75', memory: '256MiB' },
  restart_policy: 'unless-stopped',
  labels: { 'io.podorel.template': 'redis-cache' },
  ui_notes: ['Persists Redis data in the redis-data volume.']
};

const redisComposeStack = {
  id: 'example-redis-cache',
  version: '1.0.0',
  name: 'Redis Cache Compose',
  description: 'Single Redis service compose stack with append-only persistence.',
  source_path: 'server/templates/compose/examples/redis-cache',
  compose_files: ['docker-compose.yml'],
  services: [
    {
      name: 'redis',
      image: 'docker.io/library/redis:7-alpine',
      restart: 'unless-stopped',
      ports: ['6379:6379']
    }
  ],
  environment_files: [],
  required_files: [],
  notes: ['Persists Redis data in the redis-data volume.'],
  labels: { 'io.podorel.compose': 'redis-cache' }
};

const runtimeMode = {
  mode: 'development',
  raw_traces_available: true,
  production_safe_summary: false
};

const systemStatus = {
  runtime_mode: 'development',
  public_url: 'http://localhost:18080',
  active_backend_port: '18080',
  ui_build_timestamp: now,
  primary_agent_health: { status: 'ok', podman_socket: 'ok', token: 'ok' },
  podman_availability: { status: 'ok' },
  fallback_mode: 'live',
  dev_supervisor: {
    status: 'running',
    supervisor_mode: 'detached',
    message: 'E2E supervisor is running.'
  }
};

const traces = [
  {
    id: 21,
    created_at: now,
    mode: 'development',
    component: 'web',
    operation: 'pods.refresh',
    correlation_id: 'corr-e2e-pod',
    agent_id: 'primary',
    target_type: 'pod',
    target_id: 'pod-e2e',
    trace: { fixture: true }
  }
];

const passkey = {
  id: 'pk-e2e',
  user_id: 'admin',
  credential_id: 'credential-e2e',
  name: 'E2E passkey',
  created_at: now,
  updated_at: now,
  last_used_at: null
};

function ok(data, correlationId = 'corr-e2e') {
  return {
    ok: true,
    data,
    error: null,
    correlation_id: correlationId
  };
}

function fail(code, message, status = 401, correlationId = 'corr-e2e-auth') {
  return {
    ok: false,
    data: null,
    error: {
      code,
      message,
      details: {}
    },
    correlation_id: correlationId
  };
}

function routeJSON(route, data) {
  return route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(ok(data))
  });
}

async function installFixtureRoutes(page) {
  let passkeys = [passkey];
  let podTemplates = [redisTemplate];

  if (typeof page.routeWebSocket === 'function') {
    await page.routeWebSocket('**/api/ws/logs**', (ws) => {
      ws.send(JSON.stringify(logLines[0]));
    });
    await page.routeWebSocket('**/api/ws/builds**', (ws) => {
      ws.send(JSON.stringify({ build: { build_id: 'build-e2e', status: 'complete', image_name: 'podorel-e2e-image:latest' } }));
      setTimeout(() => ws.close(), 150);
    });
  }

  await page.route('**/api/auth/login', async (route) => {
    const payload = JSON.parse(route.request().postData() || '{}');
    if (payload.username === 'admin' && isAcceptedAdminPassword(payload.password)) {
      await route.fallback();
      return;
    }
    await route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify(fail('AUTH_FAILED', 'Invalid credentials.'))
    });
  });
  await page.route('**/api/auth/login-agent-token', async (route) => {
    await route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify(fail('AUTH_FAILED', 'Invalid credentials.'))
    });
  });
  await page.route('**/api/auth/passkeys', async (route) => {
    if (route.request().method() === 'GET') {
      await routeJSON(route, passkeys);
      return;
    }
    await route.fallback();
  });
  await page.route('**/api/auth/passkeys/pk-e2e', async (route) => {
    if (route.request().method() === 'DELETE') {
      passkeys = passkeys.filter((item) => item.id !== 'pk-e2e');
      await routeJSON(route, { deleted: true });
      return;
    }
    await route.fallback();
  });
  await page.route('**/api/system/tls-ca**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/x-pem-file',
      body: '-----BEGIN CERTIFICATE-----\nE2E\n-----END CERTIFICATE-----\n'
    });
  });
  await page.route('**/api/system/status', (route) => routeJSON(route, systemStatus));
  await page.route('**/api/health', (route) => routeJSON(route, { status: 'ok', mode: 'development', service: 'podorel-web' }));
  await page.route('**/api/agents', async (route) => {
    if (route.request().method() === 'GET') {
      await routeJSON(route, [primaryAgent, extraAgent]);
      return;
    }
    await route.fallback();
  });
  await page.route('**/api/agents/register', async (route) => {
    if (route.request().method() === 'POST') {
      await routeJSON(route, { agent: extraAgent, token: 'podorel-e2e-agent-token' });
      return;
    }
    await route.fallback();
  });
  await page.route('**/api/agents/*/rotate-token', (route) => routeJSON(route, { agent: primaryAgent, token: 'podorel-e2e-rotated-token' }));
  await page.route('**/api/agents/*/health', (route) => routeJSON(route, {
    status: 'ok',
    mode: 'development',
    user: 'curly',
    podman_socket: { ok: true, value: 'connected' },
    podman_cli: { ok: true, value: 'available' },
    token: { ok: true, value: 'accepted' },
    metadata: {
      agent_user: 'curly',
      agent_mode: 'development',
      socket_path: primaryAgent.socket_path,
      last_seen_at: now
    }
  }));
  await page.route('**/api/pods', async (route) => {
    if (route.request().method() === 'GET') {
      await routeJSON(route, [runningPod]);
      return;
    }
    await route.fallback();
  });
  await page.route('**/api/pods/pod-e2e', async (route) => {
    if (route.request().method() === 'GET') {
      await routeJSON(route, { pod: stripViewFields(runningPod), containers: [runningContainer, exitedContainer] });
      return;
    }
    if (route.request().method() === 'DELETE') {
      await routeJSON(route, { deleted: true, pod_id: 'pod-e2e' });
      return;
    }
    await route.fallback();
  });
  await page.route('**/api/pods/pod-e2e/*', (route) => {
    const action = route.request().url().split('/').pop();
    return routeJSON(route, { pod_id: 'pod-e2e', action, state: action === 'stop' ? 'stopped' : 'running' });
  });
  await page.route('**/api/containers', async (route) => {
    if (route.request().method() === 'GET') {
      await routeJSON(route, [runningContainer, exitedContainer]);
      return;
    }
    await route.fallback();
  });
  await page.route('**/api/containers?**', async (route) => {
    if (route.request().method() === 'GET') {
      await routeJSON(route, [runningContainer, exitedContainer]);
      return;
    }
    await route.fallback();
  });
  await page.route('**/api/containers/ctr-e2e', async (route) => {
    if (route.request().method() === 'GET') {
      await routeJSON(route, runningContainer);
      return;
    }
    if (route.request().method() === 'DELETE') {
      await routeJSON(route, { deleted: true, container_id: 'ctr-e2e' });
      return;
    }
    await route.fallback();
  });
  await page.route('**/api/containers/ctr-e2e/*', (route) => {
    const action = route.request().url().split('/').pop();
    return routeJSON(route, { container_id: 'ctr-e2e', action, state: action === 'stop' ? 'stopped' : 'running' });
  });
  await page.route('**/api/stats/current', (route) => routeJSON(route, runningPod.stats));
  await page.route('**/api/stats/history**', (route) => routeJSON(route, runningPod.stats));
  await page.route('**/api/logs/history**', async (route) => {
    if (route.request().url().includes('download=true')) {
      await route.fulfill({ status: 200, contentType: 'text/plain', body: logLines.map((line) => line.line).join('\n') });
      return;
    }
    await routeJSON(route, { lines: logLines, source: 'fixture', since: now });
  });
  await page.route('**/api/security/summary', (route) => routeJSON(route, securitySummary));
  await page.route('**/api/security/scanner-options', (route) => routeJSON(route, scannerOptions));
  await page.route('**/api/security/scan', (route) => routeJSON(route, latestScan));
  await page.route('**/api/security/scans/scan-e2e', (route) => routeJSON(route, latestScan));
  await page.route('**/api/security/findings**', (route) => routeJSON(route, [finding]));
  await page.route('**/api/security/image-digests', (route) => routeJSON(route, [imageDigest]));
  await page.route('**/api/security/host-updates', (route) => routeJSON(route, [hostUpdate]));
  await page.route('**/api/settings', async (route) => {
    if (route.request().method() === 'GET') {
      await routeJSON(route, settings);
      return;
    }
    if (route.request().method() === 'PUT') {
      await routeJSON(route, { updated: ['actions', 'metrics', 'logs', 'security'], requires_restart: false });
      return;
    }
    await route.fallback();
  });
  await page.route('**/api/templates', async (route) => {
    if (route.request().method() === 'GET') {
      await routeJSON(route, podTemplates);
      return;
    }
    if (route.request().method() === 'POST') {
      const template = JSON.parse(route.request().postData() || '{}');
      template.custom = true;
      podTemplates = [
        ...podTemplates.filter((item) => item.id !== template.id),
        template
      ].sort((left, right) => left.id.localeCompare(right.id));
      await routeJSON(route, { template, updated_at: now });
      return;
    }
    await route.fallback();
  });
  await page.route('**/api/templates/*', async (route) => {
    if (route.request().method() === 'DELETE') {
      const id = decodeURIComponent(route.request().url().split('/').pop() || '');
      podTemplates = podTemplates.filter((item) => item.id !== id);
      await routeJSON(route, { deleted: true, template_id: id });
      return;
    }
    await route.fallback();
  });
  await page.route('**/api/compose-stacks', async (route) => {
    if (route.request().method() === 'GET') {
      await routeJSON(route, [redisComposeStack]);
      return;
    }
    await route.fallback();
  });
  await page.route('**/api/diagnostics/runtime-mode', (route) => routeJSON(route, runtimeMode));
  await page.route('**/api/diagnostics/traces**', (route) => routeJSON(route, { traces, redacted: false }));
  await page.route('**/api/diagnostics/stats/ctr-e2e', (route) => routeJSON(route, runningPod.stats[0]));
  await page.route('**/api/diagnostics/bundle', (route) => routeJSON(route, {
    bundle_id: 'bundle-e2e',
    redacted: true,
    runtime: runtimeMode,
    system: systemStatus
  }));
  await page.route('**/api/audit**', (route) => routeJSON(route, auditEvents));
  await page.route('**/api/pods/create-from-template', async (route) => {
    const payload = JSON.parse(route.request().postData() || '{}');
    await routeJSON(route, {
      pod_id: payload.confirm ? 'pod-e2e-created' : undefined,
      preview_command: ['podman', 'run', '--detach', '--pod', `new:${payload.pod_name || 'pod'}`],
      confirm: payload.confirm === true,
      values: payload.values || {}
    });
  });
  await page.route('**/api/compose-stacks/deploy', async (route) => {
    const payload = JSON.parse(route.request().postData() || '{}');
    await routeJSON(route, {
      stack_id: payload.stack_id,
      project_name: payload.project_name,
      preview_command: ['podman-compose', 'up', '-d'],
      confirm: payload.confirm === true
    });
  });
  await page.route('**/api/images/build-from-dockerfile', async (route) => {
    const payload = JSON.parse(route.request().postData() || '{}');
    await routeJSON(route, payload.confirm
      ? { build_id: 'build-e2e', image_name: payload.image_name, status: 'queued' }
      : { requires_password: true, preview_command: ['podman', 'build', '-t', payload.image_name || 'image', '-'] });
  });
  await page.route('**/api/secrets', (route) => routeJSON(route, { secret_id: 'podorel-e2e-secret-ui-all', created: true }));
}

function isAcceptedAdminPassword(password) {
  return password === defaultAdminPassword || /^podorel-e2e-admin-password(?:-.+)?$/.test(password || '');
}

function stripViewFields(pod) {
  const { containers, stats, self_management, snapshot_source, ...plain } = pod;
  return plain;
}

module.exports = {
  auditEvents,
  exitedContainer,
  installFixtureRoutes,
  logLines,
  primaryAgent,
  runningContainer,
  runningPod
};
