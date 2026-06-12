## DockTail Cloud

DockTail Cloud reporting is optional, opt-in monitoring for DockTail-managed services across one or more hosts. The DockTail agent stays completely inert unless `DOCKTAIL_CLOUD_KEY` is set; with no key, no connection is opened and DockTail runs exactly as before.

Reporting rides along with the normal agent — there is no separate binary. The same DockTail container gains cloud reporting when a workspace key is present.

The hosted control plane (the dashboard and ingest service behind `wss://ingest.docktail.org`) is a separate, proprietary product. The reporting module in this repository only sends metadata to it over an outbound-only WebSocket Secure (WSS) connection.

### How To Enable

1. Create a workspace in the DockTail Cloud dashboard and copy the workspace key (`dtc_...`).
2. Set `DOCKTAIL_CLOUD_KEY` on the DockTail agent.

That is the only configuration — the cloud endpoint is built into the agent.

```yaml
services:
  docktail:
    image: ghcr.io/marvinvr/docktail:latest
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - /var/run/tailscale:/var/run/tailscale
    environment:
      - TAILSCALE_OAUTH_CLIENT_ID=${TAILSCALE_OAUTH_CLIENT_ID}
      - TAILSCALE_OAUTH_CLIENT_SECRET=${TAILSCALE_OAUTH_CLIENT_SECRET}
      # Optional. Enables DockTail Cloud reporting.
      - DOCKTAIL_CLOUD_KEY=${DOCKTAIL_CLOUD_KEY}
```

### Environment Variables

| Variable | Default | Description |
| --- | --- | --- |
| `DOCKTAIL_CLOUD_KEY` | - | Workspace key (`dtc_...`) from the cloud dashboard. Enables reporting. Inert when unset. |
| `DOCKTAIL_LOG_LEVEL` | `info` | Log level for the cloud module: `debug`, `info`, `warn`, or `error`. |
| `DOCKTAIL_CHECK_INTERVAL` | `30s` | How often local-vantage checks run. |

### What It Sends

When enabled, the agent reports metadata only:

- Snapshots of DockTail-managed services after each reconcile.
- Docker failure events, including container exit codes, out-of-memory (OOM) kills, health-status changes, and restart loops.
- Local-vantage check results. Checks default to TCP; HTTP checks run only when pushed from cloud-managed config.
- Opt-in log excerpts. Log capture is enabled from the cloud dashboard and pushed to the agent through the protocol; it is off by default.

### What It Never Does

- No remote command execution, deployment, or shell access. The protocol is metadata-only and has no exec, deploy, or shell message types.
- It never executes anything on the host on behalf of the cloud.
- The connection is outbound only; the agent dials the ingest endpoint and nothing dials in.

### Identity

Each host is identified by its Docker engine ID, used as a stable fingerprint. The agent does not report an FQDN; the tailnet vantage and FQDN are populated later by the cloud prober.
