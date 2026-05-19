# DockTail Documentation

DockTail exposes Docker containers as Tailscale Services using label-based configuration. It watches Docker events, reads `docktail.*` labels, and advertises matching containers through the local Tailscale daemon.

## Why DockTail?

DockTail uses native Tailscale Services, not per-container Tailscale devices.

<div class="comparison-table-wrap">
  <table class="comparison-table">
    <thead>
      <tr>
        <th scope="col">Feature</th>
        <th scope="col">DockTail</th>
        <th scope="col">TSDProxy</th>
        <th scope="col">ScaleTail</th>
        <th scope="col">tsbridge</th>
        <th scope="col">Plain Services</th>
      </tr>
    </thead>
    <tbody>
      <tr>
        <th scope="row">Native Tailscale Services</th>
        <td aria-label="Yes">✅</td>
        <td aria-label="No">❌</td>
        <td aria-label="No">❌</td>
        <td aria-label="No">❌</td>
        <td aria-label="Yes">✅</td>
      </tr>
      <tr>
        <th scope="row">Configured via Docker labels</th>
        <td aria-label="Yes">✅</td>
        <td aria-label="Yes">✅</td>
        <td aria-label="No">❌</td>
        <td aria-label="Yes">✅</td>
        <td aria-label="No">❌</td>
      </tr>
      <tr>
        <th scope="row">
          Apps do not consume separate Tailscale device slots
          <span class="info-tip" tabindex="0" aria-label="DockTail advertises apps as services from one tagged host instead of creating a separate Tailscale device identity per app. Exact plan limits depend on Tailscale.">i<span role="tooltip">DockTail advertises apps as services from one tagged host instead of creating a separate Tailscale device identity per app. Exact plan limits depend on Tailscale.</span></span>
        </th>
        <td aria-label="Yes">✅</td>
        <td aria-label="No">❌</td>
        <td aria-label="No">❌</td>
        <td aria-label="No">❌</td>
        <td aria-label="Yes">✅</td>
      </tr>
      <tr>
        <th scope="row">No app port publishing</th>
        <td aria-label="Yes">✅</td>
        <td aria-label="Depends on proxy or network setup">⚠️ <span class="info-tip" tabindex="0" aria-label="Depends on proxy and Docker network setup.">i<span role="tooltip">Depends on proxy and Docker network setup.</span></span></td>
        <td aria-label="Depends on sidecar setup">⚠️ <span class="info-tip" tabindex="0" aria-label="Depends on the sidecar template and app network setup.">i<span role="tooltip">Depends on the sidecar template and app network setup.</span></span></td>
        <td aria-label="Depends on proxy or network setup">⚠️ <span class="info-tip" tabindex="0" aria-label="Depends on proxy and Docker network setup.">i<span role="tooltip">Depends on proxy and Docker network setup.</span></span></td>
        <td aria-label="Manual backend setup required">⚠️ <span class="info-tip" tabindex="0" aria-label="You configure how the service host reaches the backend yourself.">i<span role="tooltip">You configure how the service host reaches the backend yourself.</span></span></td>
      </tr>
      <tr>
        <th scope="row">Automatic Docker reconciliation</th>
        <td aria-label="Yes">✅</td>
        <td aria-label="Yes">✅</td>
        <td aria-label="No">❌</td>
        <td aria-label="Yes">✅</td>
        <td aria-label="No">❌</td>
      </tr>
      <tr>
        <th scope="row">Low manual setup after install</th>
        <td aria-label="Yes">✅</td>
        <td aria-label="Yes">✅</td>
        <td aria-label="Template setup per app">⚠️ <span class="info-tip" tabindex="0" aria-label="ScaleTail is template-based, so each app usually starts from its own Compose recipe.">i<span role="tooltip">ScaleTail is template-based, so each app usually starts from its own Compose recipe.</span></span></td>
        <td aria-label="Yes">✅</td>
        <td aria-label="No">❌</td>
      </tr>
    </tbody>
  </table>
</div>

## What DockTail Does

- Discovers labeled Docker containers automatically.
- Proxies directly to container IPs by default, so app containers do not need published Docker ports.
- Advertises HTTP, HTTPS, TCP, and TLS-terminated TCP services through Tailscale.
- Supports Tailscale HTTPS with automatic certificates.
- Supports Tailscale Funnel for public internet access.
- Supports multiple Tailscale services from one container.
- Reconciles state when containers restart and container IPs change.
- Runs as a stateless Docker container.

## Recommended Reading Order

1. Start with [Quick Start](#quick-start) for a minimal Compose setup.
2. Read [Installation](#installation) for host Tailscale and sidecar options.
3. Configure Tailscale permissions in [Tailscale Admin Setup](#tailscale-admin-setup).
4. Use [Labels](#labels) and [Examples](#examples) when exposing real services.
5. Check [Reference](#reference) for all labels, environment variables, protocols, and behavior notes.
