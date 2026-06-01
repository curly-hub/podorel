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

## Cannot Reach Production URL From Another Machine

If `curl -I http://127.0.0.1:PORT/` works on the host but another machine cannot
reach `http://HOST:PORT/`, first confirm the Podman publisher is listening on all
host interfaces:

```bash
ss -ltnp | grep ':PORT'
```

The listener should show `*:PORT` or `0.0.0.0:PORT`. If it does, PoDorel is up
and the remaining issue is usually the host firewall, the wrong LAN IP, mDNS, or
network isolation. Fedora installs open the chosen port in firewalld when it is
running unless `PODOREL_SKIP_FIREWALL=1` is set. For other firewalls, allow the
chosen port manually, then test the LAN IP directly before testing `.local` DNS.

```bash
sudo ufw allow PORT/tcp
curl -I http://LAN-IP:PORT/
```

On Fedora or other firewalld-based systems, use `firewall-cmd` instead of `ufw`:

```bash
sudo firewall-cmd --permanent --add-port=PORT/tcp
sudo firewall-cmd --reload
```

## Trivy Cannot Find Local Podman Images

Production scans run through the host agent. For local Podman images, the agent
exports a temporary `podman image save` archive and asks Trivy to scan that file,
so rootless `podman.socket` is not required just for image scanning. If scans
still fail with messages about Docker, containerd, Podman sockets, or remote
registry authentication, restart both PoDorel services after updating so the web
process and host agent are on the same version.

```bash
systemctl --user restart podorel-agent.service
systemctl --user restart podorel-web.service
```

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
