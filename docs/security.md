# Security

PoDorel is designed for rootless, single-user Podman administration. The web/API
service should not control Docker, Kubernetes, rootful Podman, or remote cloud
services. Host Podman access is delegated to a per-user agent over a local Unix
socket.

## Important Deployment Notes

- PoDorel serves HTTP by default. Use a trusted reverse proxy, VPN, or tunnel for HTTPS and remote access.
- Keep the generated admin password and agent token files private to the target Linux user.
- Review support bundles and downloaded logs before sharing them publicly.
- Treat Compose imports like source code: review `.env.example`, mounts, build contexts, and scripts before deploying.

## Implemented Controls

- Explicit `--development` and `--production` runtime modes.
- Argon2id password hashing for admin login.
- HTTP-only session cookies and CSRF protection for state-changing requests.
- Hashed agent tokens and bearer-token validation with constant-time comparison.
- Redaction helpers for sensitive fields, tokens, passwords, secrets, and authorization values.
- Audit events for authentication, lifecycle actions, settings, secrets metadata, and security operations.
- Production operational logging emits errors only.

Outstanding security work and production limits are tracked in
[limitations.md](limitations.md).
