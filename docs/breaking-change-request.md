# Compatibility Policy

PoDorel is pre-1.0, so public interfaces may still change while the v1 shape is
settling. Changes that affect installation flags, configuration paths, database
layout, API contracts, template manifests, or CLI behavior should include a
short migration note in the relevant release or pull request.

Before removing or renaming a user-facing behavior, prefer one of these paths:

1. Add a compatibility alias or warning for one release.
2. Document the migration in `docs/operations.md` or `docs/limitations.md`.
3. Update installer dry-run checks and tests so the new behavior is visible.
