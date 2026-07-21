## DockTail Cloud

DockTail Cloud reporting is optional, opt-in monitoring for DockTail-managed services across one or more hosts. The DockTail agent stays completely inert unless `DOCKTAIL_CLOUD_KEY` is set; with no key, no connection is opened and DockTail runs exactly as before.

[Explore DockTail Cloud](https://docktail.org/cloud/) or [open the dashboard](https://cloud.docktail.org/login).

Reporting rides along with the normal agent — there is no separate binary. The same DockTail container gains cloud reporting when a workspace key is present.

The hosted control plane (the dashboard and ingest service behind `wss://ingest.docktail.org`) is a separate, proprietary product. The reporting module in this repository sends operational metadata and bounded incident log tails to it over an outbound-only WebSocket Secure (WSS) connection. It has no exec, deploy, shell, or command path.

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

When enabled, the agent reports the following operational data:

- Periodic snapshots of DockTail-managed services, including stopped containers, plus refreshes after successful reconciles.
- A read-only inventory of the host's **other** containers — the ones *not* published with `docktail.*` labels, including stopped ones — with name, image, state/health, ports, and live CPU/memory. These containers are not actively probed; they are listed on the dashboard so you can see the host's whole Docker footprint, and can be explicitly watched for Docker-event-driven incidents and alerts.
- Docker failure events, including container exit codes, out-of-memory (OOM) kills, health-status changes, and restart loops.
- Local-vantage check results. Checks default to TCP; HTTP checks run only when pushed from cloud-managed config.
- Bounded incident log excerpts. Capture is on by default for down-signal events, capped agent-side, and can be disabled globally or per service from the cloud dashboard.

### What It Never Does

- No remote command execution, deployment, or shell access. The protocol is metadata-only and has no exec, deploy, or shell message types.
- It never executes anything on the host on behalf of the cloud.
- The connection is outbound only; the agent dials the ingest endpoint and nothing dials in.

### Identity

Each host is identified by its Docker engine ID, used as a stable fingerprint. The agent does not report an FQDN; the tailnet vantage and FQDN are populated later by the cloud prober.
