# Docker Compose Migration

PoDorel can carry Docker Compose projects as portable compose-stack templates
under `server/templates/compose`. Deploy bundles already copy
`server/templates`, so published examples and intentionally imported stacks
travel with the same archive used for a new machine install.

The repository ships one minimal public example:

```text
server/templates/compose/examples/hello-web/
```

Imported private or site-specific stacks created by
`scripts/import-compose-stacks.sh` should be reviewed before they are committed.
The script defaults to `server/templates/compose/imported`, which is meant as a
staging area rather than a guarantee that every imported file is publishable.

Each stack directory contains:

- `podorel-compose.json`: catalog metadata used by the API and UI.
- One or more compose files, usually `docker-compose.yml`.
- Safe build-context files needed by local `build:` entries.
- `.env.example` when the original compose project expected an `.env`.

Live `.env` files, runtime data, secrets directories, node modules, generated
build output, databases, logs, and large licensed/runtime assets should stay out
of committed compose templates. Before deploying a migrated stack on a new host,
create the required `.env` file from `.env.example` and restore any
intentionally excluded runtime data or licensed blobs.

The agent deploys a compose stack with `podman compose` when available, falling
back to `podman-compose` when that binary is installed. Compose bundles are
staged on the agent host under `~/.local/share/podorel/compose-stacks/<project>`
by default. Set `PODOREL_AGENT_COMPOSE_DIR` on the agent to use a different
persistent storage root.
