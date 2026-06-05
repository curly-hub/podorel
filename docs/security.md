# Security

PoDorel is designed for rootless, single-user Podman administration. The web/API
service should not control Docker, Kubernetes, rootful Podman, or remote cloud
services. Host Podman access is delegated to a per-user agent over a local Unix
socket.

## Important Deployment Notes

- PoDorel serves HTTP by default unless native TLS is configured with
  `PODOREL_TLS_CERT_FILE` and `PODOREL_TLS_KEY_FILE`.
- Set `PODOREL_TLS_CA_FILE` or place `podorel-local-ca.crt` beside the server
  certificate to expose a browser-downloadable local CA for passkey setup.
- When native TLS is enabled, PoDorel redirects HTTP requests on the same public
  port to HTTPS.
- Passkeys require a browser-secure context. Use HTTPS with a certificate trusted
  by the browser, or localhost for development.
- If TLS terminates before PoDorel, set `PODOREL_PUBLIC_URL` to `https://...`
  and enable trusted proxy mode only for a proxy you control.
- Keep the generated admin password and agent token files private to the target Linux user.
- Review support bundles and downloaded logs before sharing them publicly.
- Treat Compose imports like source code: review `.env.example`, mounts, build contexts, and scripts before deploying.

## Implemented Controls

- Explicit `--development` and `--production` runtime modes.
- Argon2id password hashing for admin login.
- Native HTTPS support when certificate and key files are configured.
- HTTP-to-HTTPS redirect when native TLS is active.
- Passkey registration and login for secure browser contexts.
- HTTP-only session cookies and CSRF protection for state-changing requests.
- Hashed agent tokens and bearer-token validation with constant-time comparison.
- Redaction helpers for sensitive fields, tokens, passwords, secrets, and authorization values.
- Audit events for authentication, lifecycle actions, settings, secrets metadata, and security operations.
- Production operational logging emits errors only.

Outstanding security work and production limits are tracked in
[limitations.md](limitations.md).
