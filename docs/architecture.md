# Architecture

PoDorel separates the browser-facing control plane from direct host Podman
access. The web/API service handles auth, UI traffic, SQLite state, audit trails,
templates, and diagnostics. A host-side user agent performs Podman operations for
the same Linux user.

```text
Browser UI
  -> PoDorel web/API service, Go, rootless Podman container
  -> Unix socket + bearer token
  -> PoDorel agent, Go, systemd user service
  -> Rootless Podman socket preferred, Podman CLI fallback
```

## Components

- `ui/`: Angular Material frontend served by the web/API service in production.
- `server/`: Go HTTP API, auth, audit log, diagnostics, templates, Compose catalog, and SQLite store.
- `agent/`: host-side API over a Unix socket for Podman pod, container, logs, stats, secret, and compose actions.
- `cmd/podorel/`: small local CLI wrapper for status, logs, service lifecycle, and diagnostics.
- `packaging/`: rootless Podman and systemd user-service packaging.

## Runtime Modes

Development mode favors local iteration and extra diagnostics. Production mode
uses explicit production defaults, quieter operational logging, and the packaged
rootless Podman plus systemd layout.

Known limits are tracked in [limitations.md](limitations.md).
