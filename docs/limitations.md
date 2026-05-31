# Limitations

PoDorel is an early v1 project. The core local Podman workflow is implemented
and tested, but these areas are still evolving.

Known limits:

1. Production support is intended for Debian, Ubuntu, and Fedora, but full distro-matrix CI validation is still pending.
2. Security scans run Trivy when it is installed and record scanner-unavailable failures otherwise. Image remote digest checks require `skopeo` when available, and host package checks are best-effort through `apt` or `dnf`.
3. Dockerfile imports create durable `image_builds` records and expose a build-status WebSocket by `build_id`, but full line-by-line Podman build output streaming and multi-file build-context archives are still incomplete.
4. Docker Compose stacks can be cataloged, previewed, bundled, and deployed through the agent with `podman compose` or `podman-compose`; editing compose files and `.env` values directly in the UI is still incomplete.
5. Pod creation supports validated template values and proxies creation through the agent, but template value schemas are not yet first-class metadata in template files.
6. Real Podman integration tests remain opt-in with `PODOREL_RUN_REAL_PODMAN_TESTS=1` and clean `podorel-test-*` resources.
7. Browser E2E journeys are not yet implemented; `scripts/test-all.sh` currently covers Go tests, UI checks/build, fake-agent integration, and deployment dry runs.
8. Idle memory and resource budget checks are not yet complete.

These are tracked so operators can judge fit before relying on PoDorel in
production.
