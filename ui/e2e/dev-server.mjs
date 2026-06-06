import { spawn, spawnSync } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { closeSync, existsSync, mkdirSync, openSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const uiDir = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const rootDir = resolve(uiDir, '..');
const uiURL = process.env.PODOREL_E2E_UI_URL || 'http://localhost:14200';
const apiURL = process.env.PODOREL_E2E_API_URL || 'http://localhost:18080';
const uiPort = new URL(uiURL).port || '14200';
const apiListen = new URL(apiURL).host;
const tempRoot = process.env.PODOREL_E2E_DIR || join('/tmp', `podorel-ui-e2e-${uiPort}-${process.pid}`);
const runDir = join(tempRoot, 'run');
const configDir = join(tempRoot, 'config');
const dataDir = join(tempRoot, 'data');
const logDir = join(tempRoot, 'logs');
const tokenFile = join(configDir, 'agent-token');
const socketPath = join(runDir, 'podorel-agent.sock');
const proxyConfig = join(tempRoot, 'proxy.conf.json');
const statusFile = join(tempRoot, 'dev-status.json');
const children = [];

mkdirSync(runDir, { recursive: true });
mkdirSync(configDir, { recursive: true });
mkdirSync(dataDir, { recursive: true });
mkdirSync(logDir, { recursive: true });

if (!existsSync(tokenFile)) {
  writeFileSync(tokenFile, randomBytes(32).toString('base64'), { mode: 0o600 });
}

writeFileSync(proxyConfig, JSON.stringify({
  '/api': {
    target: apiURL,
    secure: false,
    changeOrigin: true,
    ws: true
  }
}, null, 2));

function startProcess(name, command, args, options = {}) {
  const logPath = join(logDir, `${name}.log`);
  const log = openSync(logPath, 'a');
  const child = spawn(command, args, {
    cwd: options.cwd || rootDir,
    detached: true,
    env: {
      ...process.env,
      NG_CLI_ANALYTICS: 'false',
      ...options.env
    },
    stdio: ['ignore', log, log]
  });
  closeSync(log);
  children.push({ name, child });
  child.once('exit', (code, signal) => {
    if (!shuttingDown) {
      console.error(`${name} exited early with code ${code ?? 'null'} signal ${signal ?? 'null'}. Log: ${logPath}`);
      shutdown(1);
    }
  });
  console.log(`${name}: pid ${child.pid}, log ${logPath}`);
  return child;
}

function token() {
  return readFileSync(tokenFile, 'utf8').trim();
}

function wait(ms) {
  return new Promise((resolveWait) => setTimeout(resolveWait, ms));
}

async function waitForAgent(child) {
  const header = `Authorization: Bearer ${token()}`;
  for (let attempt = 0; attempt < 100; attempt += 1) {
    ensureRunning('agent', child);
    const result = spawnSync('curl', [
      '-fsS',
      '--max-time',
      '2',
      '--unix-socket',
      socketPath,
      '-H',
      header,
      'http://podorel-agent/health'
    ], { stdio: 'ignore' });
    if (result.status === 0) {
      console.log('agent: healthy');
      return;
    }
    await wait(250);
  }
  throw new Error(`agent did not become healthy at ${socketPath}`);
}

async function waitForURL(name, child, url) {
  for (let attempt = 0; attempt < 180; attempt += 1) {
    ensureRunning(name, child);
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 2000);
    try {
      const response = await fetch(url, { signal: controller.signal });
      if (response.ok) {
        console.log(`${name}: healthy at ${url}`);
        clearTimeout(timeout);
        return;
      }
    } catch {
      // Keep polling until the process exits or the timeout is reached.
    } finally {
      clearTimeout(timeout);
    }
    await wait(500);
  }
  throw new Error(`${name} did not become healthy at ${url}`);
}

function ensureRunning(name, child) {
  if (child.exitCode !== null || child.signalCode !== null) {
    throw new Error(`${name} exited before it became healthy`);
  }
}

let shuttingDown = false;

async function cleanup() {
  shuttingDown = true;
  for (const { child } of [...children].reverse()) {
    if (!child.pid) {
      continue;
    }
    try {
      process.kill(-child.pid, 'SIGTERM');
    } catch {
      try {
        child.kill('SIGTERM');
      } catch {
        // Process already exited.
      }
    }
  }
  await wait(1500);
  for (const { child } of [...children].reverse()) {
    if (!child.pid) {
      continue;
    }
    try {
      process.kill(-child.pid, 'SIGKILL');
    } catch {
      // Process already exited.
    }
  }
  spawnSync('podman', ['secret', 'rm', 'podorel-e2e-secret', 'podorel-e2e-secret-ui-all'], { stdio: 'ignore' });
  spawnSync('podman', ['rmi', 'podorel-e2e-image:latest'], { stdio: 'ignore' });
}

function shutdown(status) {
  cleanup().finally(() => process.exit(status));
}

process.on('SIGINT', () => shutdown(130));
process.on('SIGTERM', () => shutdown(143));

try {
  console.log(`E2E temp root: ${tempRoot}`);
  const commonGoEnv = { GOCACHE: process.env.GOCACHE || '/tmp/podorel-go-cache' };
  const agent = startProcess('agent', 'go', [
    'run',
    './agent/cmd/podorel-agent',
    '--development',
    '--socket-path',
    socketPath,
    '--token-file',
    tokenFile
  ], { env: commonGoEnv });
  await waitForAgent(agent);

  const web = startProcess('web', 'go', [
    'run',
    './server/cmd/podorel-web',
    '--development',
    '--listen-addr',
    apiListen,
    '--public-url',
    apiURL,
    '--db-path',
    join(dataDir, 'podorel.db'),
    '--ui-dist-path',
    'ui/dist/podorel-ui/browser'
  ], {
    env: {
      ...commonGoEnv,
      PODOREL_AGENT_SOCKET: socketPath,
      PODOREL_AGENT_TOKEN_FILE: tokenFile,
      PODOREL_LOG_DIR: logDir,
      PODOREL_DEV_STATUS_FILE: statusFile
    }
  });
  await waitForURL('web', web, `${apiURL}/api/health`);

  const ngBinary = join(uiDir, 'node_modules', '.bin', 'ng');
  const ui = startProcess('ui', ngBinary, [
    'serve',
    '--host',
    'localhost',
    '--port',
    uiPort,
    '--proxy-config',
    proxyConfig
  ], { cwd: uiDir });
  await waitForURL('ui', ui, uiURL);

  console.log(`PoDorel E2E dev stack ready: ${uiURL}`);
  setInterval(() => {}, 60_000);
} catch (error) {
  console.error(error instanceof Error ? error.stack || error.message : error);
  shutdown(1);
}
