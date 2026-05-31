# PoDorel

PoDorel is a local web console for rootless Podman pods. It gives a single
Linux user a browser UI for pod and container lifecycle actions, logs, basic
stats, templates, Compose stack deployment, security scan results, audit logs,
and diagnostics.

PoDorel runs as two cooperating pieces: a Go web/API service in a rootless
Podman pod, and a per-user host agent that talks to the user's rootless Podman
socket or CLI. State is stored in SQLite. The UI is built with Angular Material.

## For Users

### Supported Systems

The v1 installer supports Debian, Ubuntu, and Fedora. Unsupported distributions
fail fast with a clear message.

PoDorel serves HTTP by default on a LAN-oriented address. Put it behind a trusted
reverse proxy, VPN, or tunnel if you need HTTPS or remote access.

### Install

Install from a fresh clone on the target machine:

```bash
git clone https://github.com/curly-hub/podorel.git
cd podorel
./install.sh --yes --public-url http://podorel.lan:8080
```

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
