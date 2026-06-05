<p align="center">
  <img src="ui/public/logo.svg" alt="PoDorel" width="220">
</p>

<h1 align="center">PoDorel</h1>

<p align="center">
  <strong>The calm control room for rootless Podman.</strong>
</p>

<p align="center">
  Operate pods, inspect logs, deploy templates and Compose stacks, watch
  resource usage, check security posture, and troubleshoot local Podman hosts
  from one focused web console.
</p>

<p align="center">
  <a href="docs/podorel-presentation.md"><strong>Open the presentation</strong></a>
  ·
  <a href="docs/operations.md">Operations</a>
  ·
  <a href="docs/security.md">Security</a>
  ·
  <a href="docs/architecture.md">Architecture</a>
  ·
  <a href="docs/limitations.md">Limits</a>
</p>

![PoDorel product presentation banner](docs/podorel-presentation-preview.svg)

PoDorel is a local web console for rootless Podman pods. It gives a Linux user
a browser UI for lifecycle actions, logs, resource stats, pod templates, Compose
stack deployment, security scan results, image digest checks, audit logs,
settings, agents, and diagnostics.

PoDorel is intentionally local-first. It is not a Kubernetes replacement and it
does not require a remote control plane. It gives rootless Podman a practical,
human-facing operations surface.

## Why PoDorel

| Need | What PoDorel gives you |
| --- | --- |
| Clear state | Dashboard, pod cards, container detail, logs, stats, agent health, diagnostics, and audit history. |
| Safer operations | HTTPS-ready config, passkeys, CSRF protection, admin-gated settings, scale warnings, memory visibility, and audit events. |
| Repeatable creation | Pod templates, Compose stack deployment, Dockerfile builds, secrets metadata, and reusable examples. |
| Rootless by design | A per-user host agent talks to the correct rootless Podman socket or CLI without turning the web UI into root. |
| Small-team practicality | Useful for home labs, dev hosts, internal tools, and edge boxes where a full cluster is too much. |

## Highlights

- **Pods and containers:** start, stop, restart, kill, inspect, and delete.
- **Logs:** browse recent logs and download support-friendly log windows.
- **Stats:** CPU and memory visibility with clearer resource-limit context.
- **Templates:** create pods from curated templates with safer defaults.
- **Compose:** deploy Compose stacks from reviewed templates.
- **Agents:** inspect web, token, socket, agent API, and Podman health layers.
- **Security:** scan status, image digest checks, host package updates, and audit logs.
- **Settings:** operations toggles, retention controls, HTTPS posture, passkeys, and unsaved-change visibility.
- **Diagnostics:** runtime profile, traces, support bundles, and correlation IDs.

## Architecture

PoDorel runs as two cooperating pieces:

| Piece | Role |
| --- | --- |
| Go web/API service | Serves the UI, API, auth, audit, settings, SQLite state, templates, and diagnostics. |
| Per-user host agent | Runs beside the Linux user and talks to that user's rootless Podman socket or CLI. |
| SQLite | Stores sessions, agents, audit events, scans, settings, templates, and local metadata. |
| Angular UI | Provides the browser console for daily operations. |

The web/API service is installed in a rootless Podman pod. The host agent stays
outside the pod because it needs access to the user's Podman socket and local
Unix socket.

## HTTPS And Passkeys

PoDorel can serve native HTTPS when both TLS files are configured:

```bash
PODOREL_TLS_CERT_FILE=/path/to/podorel.crt \
PODOREL_TLS_KEY_FILE=/path/to/podorel.key \
./install.sh --yes --public-url https://podorel.lan:9095
```

When native TLS is enabled, PoDorel redirects HTTP requests on the same public
port to HTTPS. If TLS terminates in a reverse proxy instead, set the public URL
to `https://...` and enable trusted proxy mode only for a proxy you control.

Passkeys require a browser-secure context. Use `https://...` with a certificate
trusted by the browser, or `localhost` for development. If you use a local CA,
trust the CA in your OS/browser before registering a passkey.

## Install

Supported installer targets: Debian, Ubuntu, and Fedora.

Install from a fresh clone on the target machine as the Linux user that should
own rootless Podman. The installer asks for sudo only when it needs system-level
changes. If you are already root, pass `--target-user USER`.

```bash
git clone https://github.com/curly-hub/podorel.git
cd podorel
./install.sh --yes --public-url http://podorel.lan:8080
```

Use HTTPS by providing TLS files and an HTTPS public URL:

```bash
PODOREL_TLS_CERT_FILE=/home/alice/.local/share/podorel/tls/podorel.crt \
PODOREL_TLS_KEY_FILE=/home/alice/.local/share/podorel/tls/podorel.key \
./install.sh --yes --public-url https://podorel.lan:9095
```

Preview the install plan without changing the machine:

```bash
./install.sh --dry-run --yes
```

If `--admin-password` is omitted, the installer generates one and writes it to
`~/.config/podorel/generated-admin-password` for the target user. Sign in with
username `admin`.

If the public URL includes an explicit port, such as
`https://curly-hub.local:9095`, the installer publishes and listens on that port
unless `--listen-addr` is also supplied. On Fedora with firewalld running, the
installer opens that TCP port unless `PODOREL_SKIP_FIREWALL=1` is set.

## Day-To-Day Commands

After installation, the `podorel` CLI wraps common local operations:

```bash
podorel status
podorel logs
podorel restart
podorel stop
podorel start
podorel agent status
podorel doctor
```

More operational notes are in [docs/operations.md](docs/operations.md).
Troubleshooting notes are in [docs/troubleshooting.md](docs/troubleshooting.md).

## Current Scope

PoDorel is an early v1 project. The core local Podman workflow is implemented
and tested, but production-hardening and UI depth are still evolving. Read
[docs/limitations.md](docs/limitations.md) before relying on it for important
production workloads.

## For Developers

Repository layout:

- `server/`: Go web/API server, SQLite migrations, templates, and embedded UI support.
- `agent/`: host-side user agent and Podman runtime adapters.
- `ui/`: Angular Material frontend.
- `cmd/podorel/`: local CLI wrapper.
- `packaging/`: Podman and systemd packaging files.
- `scripts/`: install, deploy, test, and export entrypoints.
- `docs/`: architecture, operations, security, troubleshooting, and known limits.

Run the main check suite:

```bash
scripts/test-all.sh
```

The test harness runs Go formatting checks, `go vet`, Go tests, UI architecture
checks, the Angular build when `ui/node_modules` is installed, shell syntax
checks, packaging checks, and installer/deployment dry runs. Real Podman
integration tests are opt-in with `PODOREL_RUN_REAL_PODMAN_TESTS=1`.

Run the development stack:

```bash
scripts/deploy-dev.sh
```

Or run the two Go services directly:

```bash
go run ./server/cmd/podorel-web --development
go run ./agent/cmd/podorel-agent --development
```

Development defaults to `http://localhost:8080` for the Go web/API server and
`http://localhost:4200` for the Angular dev server. The development admin login
is `admin` with password `podorel-development-password`.

Build a deploy bundle:

```bash
scripts/build-deploy.sh --force
```

The generated `deploy/podorel-.../` folder and matching `.tar.gz` include
prebuilt binaries, the Angular UI, migrations, pod templates, Compose examples,
systemd units, and `install.sh`. GitHub Actions can produce the same archive
with [.github/workflows/build-deploy.yml](.github/workflows/build-deploy.yml).

Create a clean git-ready copy outside this working tree:

```bash
scripts/git-export.sh --target ../PoDorel-git-ready
```

## Compose Templates

Docker Compose migration details are in
[docs/docker-compose-migration.md](docs/docker-compose-migration.md). To scan an
existing Docker/Portainer-style tree into PoDorel Compose templates, use:

```bash
scripts/import-compose-stacks.sh --source /path/to/docker-projects
```

Review imported stacks before committing them. Live `.env` files, runtime data,
secrets, build output, databases, logs, and large runtime assets should stay out
of the repository.

## License

Copyright (c) 2026 Curly Hub.

PoDorel is dual-licensed:

- Open-source license: GNU Affero General Public License v3.0 or later (`AGPL-3.0-or-later`), see [LICENSE](LICENSE).
- Commercial license: required for proprietary, closed-source, or otherwise non-AGPL use, see [COMMERCIAL-LICENSE.md](COMMERCIAL-LICENSE.md).

AGPL-compliant users may use, modify, distribute, and run PoDorel over a
network, but corresponding source code for modified or combined works must
remain available under AGPL-compatible terms. A paid commercial license is
required to keep modifications or combined works closed source.
