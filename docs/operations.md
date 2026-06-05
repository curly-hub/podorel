# Operations

PoDorel provides a single-command installer and a `podorel` CLI wrapper for
common local operations.

## Install

```bash
./install.sh --yes --public-url http://podorel.lan:8080
```

Use native HTTPS by providing both TLS files and an HTTPS public URL:

```bash
PODOREL_TLS_CERT_FILE=/home/alice/.local/share/podorel/tls/podorel.crt \
PODOREL_TLS_KEY_FILE=/home/alice/.local/share/podorel/tls/podorel.key \
./install.sh --yes --public-url https://podorel.lan:9095
```

When native TLS is enabled, PoDorel redirects HTTP requests on the same public
port to HTTPS. If a reverse proxy terminates TLS, use an `https://` public URL
and enable trusted proxy mode only for that trusted proxy.

If the public URL includes an explicit port, such as
`https://curly-hub.local:9095`, the installer publishes and listens on that port
unless `--listen-addr` is also supplied. On Fedora with firewalld running, the
installer also opens that TCP port; set `PODOREL_SKIP_FIREWALL=1` to skip it.
Other firewalls may still need a manual allow rule.

Run the installer as the Linux user that should own rootless Podman. The script
will ask for sudo when it needs system-level changes. If you are already in a
root shell, pass `--target-user USER` so production services are not installed
for root.

Use a dry run to check prerequisites and generated steps without installing:

```bash
./install.sh --dry-run --yes
```

If no admin password is supplied, the installer generates one and writes it to
`~/.config/podorel/generated-admin-password` for the target user. The default
admin username is `admin`.

## CLI

```bash
podorel status
podorel logs
podorel restart
podorel stop
podorel start
podorel agent status
podorel doctor
```

Agent registration and token rotation are exposed in the web UI under the Agents
page. The CLI currently prints the matching API/UI path for those operations.

## Services

Production installs systemd user services for the web pod and host agent. Useful
commands on the target user account include:

```bash
systemctl --user status podorel-web.service podorel-agent.service
journalctl --user -u podorel-web.service -n 100 --no-pager
journalctl --user -u podorel-agent.service -n 100 --no-pager
```

## Support Logs

In the web UI, open **Logs** and click **Get Logs** to download the currently
selected 24-hour log window as a text file for support. Logs can include
hostnames, container names, image names, paths, and correlation IDs, so review
the file before sharing it publicly.

The same log path is available from Diagnostics under the support tab.
