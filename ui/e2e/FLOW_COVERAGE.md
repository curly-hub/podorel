# PoDorel UI E2E Flow Coverage

This document tracks the executable browser coverage in `ui/e2e/podorel-ui.spec.js`.

Run it with:

```bash
npm run e2e:dev
```

The command starts an isolated dev stack with a temporary agent socket, Go web API, Angular dev server, deterministic Playwright fixtures, browser console/page-error guards, download checks, and mocked WebSocket streams.

GitHub Actions runs the same suite through `.github/workflows/ui-e2e.yml`.

## Route Coverage

Every primary app route is visited from the UI:

- `/login`
- `/dashboard`
- `/pods`
- `/pods/:id`
- `/containers/:id`
- `/logs`
- `/security`
- `/create-pod`
- `/templates`
- `/agents`
- `/settings`
- `/audit`
- `/diagnostics`

## Flow 1: Auth, Shell, And Navigation

Covered UI:

- Auth guard redirect from `/pods` to `/login?returnUrl=%2Fpods`
- Login tabs: `Agent Token`, `Password`
- Login labels: `Username`, `Password`, `Agent token`
- Disabled/enabled login button states
- Admin password login
- Forced admin password change redirect to `/settings?changePassword=1`
- Forced-change labels: `Current admin password`, `New admin password`, `Confirm new admin password`
- Forced-change button: `Change password`
- Post-change navigation and sign out
- App shell navigation toggle open/closed
- Theme toggle button
- Sidebar links for every route
- Header icon navigation to `Settings` and `Diagnostics`

Covered APIs:

- `POST /api/auth/login`
- `POST /api/auth/login-agent-token`
- `POST /api/auth/change-password`
- `POST /api/auth/logout`
- `GET /api/auth/me`

## Flow 2: Dashboard, Pods, Pod Detail, Container Detail

Covered UI:

- Dashboard refresh and help tooltips
- Pods refresh, search, state filter, create navigation
- Pods list link to pod detail
- Pods page shell shortcut to container shell
- Pod detail shell button and row shell button
- Pod detail tabs: `Overview`, `Stats`, `Logs`, `Security`, `Actions`, `Rules`, `Audit`
- Container row actions: start, stop, restart, kill, delete
- Pod action tab buttons: stop, restart, kill, delete
- Confirmation dialogs, including typed destructive confirmation
- Container detail refresh
- Container detail tabs: `Metadata`, `Stats`, `Logs`, `Shell`, `Actions`, `Audit`
- Shell selector, disabled `Open Shell`, `Clear`, disabled `Ctrl-C`, disabled `Close`
- Container action tab buttons: stop, restart, kill, delete

Covered APIs:

- `GET /api/health`
- `GET /api/system/status`
- `GET /api/pods`
- `GET /api/pods/:id`
- `POST /api/pods/:id/start|stop|restart|kill`
- `DELETE /api/pods/:id`
- `GET /api/containers`
- `GET /api/containers/:id`
- `POST /api/containers/:id/start|stop|restart|kill`
- `DELETE /api/containers/:id`
- `GET /api/stats/current`
- `GET /api/stats/history`
- `GET /api/logs/history`
- `GET /api/security/findings`
- `GET /api/audit`

## Flow 3: Logs, Security, Audit, Agents, Settings, Diagnostics

Covered UI:

- Logs refresh and download
- Logs labels: `Search text`, `Agent`, `Pod`, `Container`, `Source`, `Lines`
- Logs apply and clear filters
- Live logs start/connect, disconnect when visible, pause/resume, clear buffer
- Historical logs tab, refresh, download
- Security refresh, rescan, help tooltip, finding display
- Audit refresh, `Search`, `Limit`
- Agents refresh
- Agent row actions: `Check health`, `Rotate token`
- Token rotation confirmation and token copy
- Agent registration labels: `Agent ID`, `Linux username`, `Linux UID`, `Socket path`
- Agent register button and returned token
- Settings refresh
- Admin password change required banner
- Admin password validation: empty current password, mismatched confirmation, invalid current password
- Admin password successful change from Settings
- Passkey reload, delete, empty state, CA download, HTTPS URL copy
- Passkey registration unavailable path with `Passkey name`
- Settings switches: `Exec shell`, `Automation rules`, `Scheduled scans`
- Settings labels: `Scan schedule`, `Metrics retention hours`, `Log retention hours`, `Log limit per pod in megabytes`, `Total log limit in megabytes`, `Admin password`
- Settings save validation and successful save
- Diagnostics refresh, runtime tab, help tooltip
- Diagnostics traces tab with `Correlation ID` and search
- Diagnostics support tab, logs download, `Admin password confirmation`, support bundle creation

Covered APIs and sockets:

- `GET /api/logs/history`
- `GET /api/logs/history?download=true`
- `WS /api/ws/logs`
- `GET /api/security/summary`
- `GET /api/security/scanner-options`
- `POST /api/security/scan`
- `GET /api/security/scans/:id`
- `GET /api/security/findings`
- `GET /api/security/image-digests`
- `GET /api/security/host-updates`
- `GET /api/audit`
- `GET /api/agents`
- `POST /api/agents/register`
- `POST /api/agents/:id/rotate-token`
- `GET /api/agents/:id/health`
- `GET /api/settings`
- `PUT /api/settings`
- `POST /api/auth/change-password`
- `GET /api/auth/passkeys`
- `DELETE /api/auth/passkeys/:id`
- `GET /api/system/tls-ca`
- `GET /api/diagnostics/runtime-mode`
- `GET /api/diagnostics/traces`
- `GET /api/diagnostics/stats/:id`
- `POST /api/diagnostics/bundle`

## Flow 4: Templates And Create Pod

Covered UI:

- Templates refresh with deterministic catalog data
- Templates labels: `Search catalog`, `Type`
- Type filter options: `Pod templates`, `Compose stacks`, `All`
- Catalog buttons: `Use`, `Deploy`, `Create Pod`
- Pod catalog draft toggle and labels: `ID`, `Name`, `Description`, `Image`, `Host port`, `Container port`, `CPU`, `Memory`, `Restart policy`, `Command lines`, `Environment lines`, `Notes`
- Pod catalog draft buttons: `Copy JSON`, `Download`
- Compose catalog draft toggle
- Compose presets: `Web`, `Web + DB`, `API + Redis`, `Blank`
- Compose catalog labels: `Stack ID`, `Name`, `Version`, `Description`, `docker-compose.yml`, `Env files`, `Required files`, `Labels`, `Notes`
- Compose catalog draft buttons: `Copy manifest`, `Copy YAML`, `Manifest`, `Compose`
- Create Pod `Templates` shortcut button
- Create Pod mode rail: `Template`, `Compose`, `Image`, `Secret`
- Template deploy labels: `Template`, `Pod name`, `Target agent`, `Find template`, host port override, extra value `Key`, extra value `Value`
- Extra Values flow: add row, fill key/value, remove row, add row again, fill key/value again
- Template deploy buttons: `Preview`, `Create Pod`
- Compose deploy labels: `Compose stack`, `Project name`, `Target agent`, `Find stack`
- Compose deploy buttons: `Preview Stack`, `Deploy Stack`
- Image build labels: `Image name`, `Target agent`, `Dockerfile`, `Password confirmation`
- Image build buttons: `Preview Build`, `Build Image`
- Secret labels: `Secret name`, `Used by pod`, `Target agent`, `Password confirmation`, `Secret value`
- Secret button: `Create Secret`
- Secret value clearing after create

Covered APIs and sockets:

- `GET /api/templates`
- `GET /api/compose-stacks`
- `POST /api/pods/create-from-template`
- `POST /api/compose-stacks/deploy`
- `POST /api/images/build-from-dockerfile`
- `WS /api/ws/builds`
- `POST /api/secrets`

## Guardrails

- Browser `pageerror` failures fail the test.
- Browser console errors fail the test, except expected auth/probe HTTP error resources.
- Downloads are accepted and verified with a suggested filename.
- Confirmation dialogs are opened and completed.
- Select controls retry when Angular re-renders Material option overlays.
- Fixture state is deterministic where UI state matters, including passkey deletion.
- The real dev auth path is used for admin login and password change.

## CI Coverage

`.github/workflows/ui-e2e.yml` provisions Go, Node, Podman, and Playwright Chromium, then runs:

```bash
npm run e2e:dev
```

The workflow uploads Playwright traces/videos/screenshots plus dev-stack logs from the temporary E2E directory on every run.

## Known Scope

This suite is UI-side E2E coverage. It verifies browser interactions, routing, labels, dialogs, downloads, WebSocket handling, state rendering, and API request/response contracts through mocked deterministic APIs plus the real dev auth/password path. Backend behavior is covered separately by Go tests.
