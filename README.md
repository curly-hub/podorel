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
</p>

<table>
  <tr>
    <td><strong>Local-first</strong><br>Designed for a Linux user running rootless Podman, not a distant control plane.</td>
    <td><strong>Visible operations</strong><br>Pods, containers, logs, metrics, agents, scans, audit logs, and diagnostics in one place.</td>
    <td><strong>Practical guardrails</strong><br>HTTPS-ready config, passkeys, CSRF protection, memory visibility, scale warnings, and admin-gated actions.</td>
  </tr>
  <tr>
    <td><strong>Templates and Compose</strong><br>Create repeatable pods and deploy Compose stacks without copy-paste shell drift.</td>
    <td><strong>Agent model</strong><br>A per-user host agent talks to the correct rootless Podman socket or CLI.</td>
    <td><strong>Small-team friendly</strong><br>Useful for home labs, dev hosts, internal tools, and edge boxes where Kubernetes is too much.</td>
  </tr>
</table>

![PoDorel product presentation banner](docs/podorel-presentation-preview.svg)

PoDorel is a local web console for rootless Podman pods. It gives a single
Linux user a browser UI for pod and container lifecycle actions, logs, basic
stats, templates, Compose stack deployment, security scan results, audit logs,
and diagnostics.

PoDorel runs as two cooperating pieces: a Go web/API service in a rootless
Podman pod, and a per-user host agent that talks to the user's rootless Podman
socket or CLI. State is stored in SQLite. The UI is built with Angular Material.

## Product Presentation

Want the quick, attractive overview first? Open the
[PoDorel presentation](docs/podorel-presentation.md) for a GitHub-rendered
visual pitch covering the problem, product, architecture, and ideal users.

## For Users

### Supported Systems

The v1 installer supports Debian, Ubuntu, and Fedora. Unsupported distributions
fail fast with a clear message.

PoDorel serves HTTP by default on a LAN-oriented address. Put it behind a trusted
reverse proxy, VPN, or tunnel if you need HTTPS or remote access.

### Install

Install from a fresh clone on the target machine. Run this as the Linux user
that should own rootless Podman; the installer will ask for sudo when it needs
system-level changes. If you are already in a root shell, pass
`--target-user USER`.

```bash
git clone https://github.com/curly-hub/podorel.git
cd podorel
./install.sh --yes --public-url http://podorel.lan:8080
```

If the public URL includes an explicit port, such as
`http://curly-hub.local:9095`, the installer publishes and listens on that port
unless `--listen-addr` is also supplied. On Fedora with firewalld running, the
installer also opens that TCP port; set `PODOREL_SKIP_FIREWALL=1` to skip it.
Other firewalls may still need a manual allow rule.

Check the installation plan without changing the machine:

```bash
./install.sh --dry-run --yes
```

If `--admin-password` is omitted, the installer generates a password and writes
it to `~/.config/podorel/generated-admin-password` for the target user. Sign in
with username `admin` and that password.

### What Gets Installed

The installer creates a rootless Podman pod named `podorel` for the web/API
container and installs the host agent as a systemd user service. The agent stays
outside the web pod because it needs access to the user's Podman socket and host
Unix socket.

### Day-to-Day Commands

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

### Current Scope

PoDorel is an early v1 project. The core local Podman workflow is implemented
and tested, but some production-hardening and UI depth are still evolving. See
[docs/limitations.md](docs/limitations.md) before relying on it for important
production workloads.

## For Developers

### Repository Layout

- `server/`: Go web/API server, SQLite migrations, templates, and embedded UI support.
- `agent/`: host-side user agent and Podman runtime adapters.
- `ui/`: Angular Material frontend.
- `cmd/podorel/`: local CLI wrapper.
- `packaging/`: Podman and systemd packaging files.
- `scripts/`: install, deploy, test, and export entrypoints.
- `docs/`: architecture, operations, security, troubleshooting, and known limits.

### Run Checks

Development and source installs require a Go 1.23-compatible toolchain.

```bash
scripts/test-all.sh
```

The test harness runs Go formatting checks, `go vet`, Go tests, UI architecture
checks, the Angular build when `ui/node_modules` is installed, shell syntax
checks, packaging checks, and installer/deployment dry runs. Real Podman
integration tests are opt-in with `PODOREL_RUN_REAL_PODMAN_TESTS=1`.

### Run Locally

Start the local development stack on HTTP localhost:

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

### Build a Deploy Bundle

```bash
scripts/build-deploy.sh --force
```

The generated `deploy/podorel-.../` folder and matching `.tar.gz` include
prebuilt binaries, the Angular UI, migrations, pod templates, compose examples,
systemd units, and `install.sh`. GitHub Actions can produce the same archive
with `.github/workflows/build-deploy.yml`.

Create a clean git-ready copy outside this working tree with:

```bash
scripts/git-export.sh --target ../PoDorel-git-ready
```

### Compose Templates

Docker Compose migration details are in
[docs/docker-compose-migration.md](docs/docker-compose-migration.md). To scan an
existing Docker/Portainer-style tree into PoDorel compose templates, use:

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
