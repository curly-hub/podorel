# Troubleshooting

Every API response includes a correlation ID. In development mode, debug traces
can include raw Podman values, parsed values, parser decisions, and command
metadata after redaction. In production mode, operational logs intentionally emit
only errors.

## Development Commands

```bash
go test ./...
scripts/test-all.sh
scripts/deploy-dev.sh
go run ./server/cmd/podorel-web --development
go run ./agent/cmd/podorel-agent --development
```

Development URLs are `http://localhost:4200` for the Angular UI and
`http://localhost:8080` for the web/API server. The API is session protected, so
`/api/pods` returning `401 AUTH_REQUIRED` is expected until the browser has a
successful login session. The default development admin login is `admin` with
`podorel-development-password`.

## Session Or CSRF Errors

The UI refreshes its state-changing request token from `/api/auth/me` when it
restores an existing cookie session. A browser reload should not require signing
out and back in before actions such as create, start, stop, scan, or settings
save. If it does, capture the correlation ID and check the web logs.

## Missing Pod Stats

If pod cards render but CPU or memory is unavailable, check the primary agent
first:

```bash
curl -b /tmp/podorel-cookies.txt http://localhost:8080/api/agents
curl -b /tmp/podorel-cookies.txt http://localhost:8080/api/pods
podman stats --no-stream --format json
```

The primary agent socket shown by `/api/agents` must match the running agent
socket. In development, `scripts/deploy-dev.sh` starts both sides with the same
`PODOREL_AGENT_SOCKET` value.

PoDorel uses `podman stats --no-stream --format json` as the stats source of
truth. Podman only returns stats rows for containers it can currently sample, so
a degraded pod with an exited application container may show usage for only the
still-running infra container. PoDorel filters out old samples from previous
refreshes instead of showing stale CPU or memory as current.
