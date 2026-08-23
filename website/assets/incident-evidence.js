/**
 * "Copy Evidence for Agent" — the demo button under the incident anatomy card
 * on /cloud.
 *
 * It puts a full sample incident export on the clipboard: the same Markdown
 * DockTail Cloud produces from the incident page (opening line, report header,
 * summary, vantage matrix, container/host facts, Docker signals, log tail,
 * health changes, timeline), describing the exact incident the card above
 * showcases — grafana on prod-01, OOM-killed at 14:22.
 *
 * Timestamps are rebuilt on every copy against "yesterday at 14:22", so the
 * ISO stamps line up with the card and the relative labels never go stale.
 * Keep the section order and wording in step with the real generator in the
 * cloud repo (web/lib/incident-markdown.ts).
 */
(function () {
  'use strict';

  var COPIED_MS = 1600;

  // ---- timestamps ---------------------------------------------------------
  // Yesterday at 14:22:07Z — the first Docker event on the card. Everything
  // else is an offset in seconds from it.
  function base() {
    var d = new Date();
    d.setUTCDate(d.getUTCDate() - 1);
    d.setUTCHours(14, 22, 7, 0);
    return d.getTime();
  }

  function iso(baseMs, offsetSeconds) {
    return new Date(baseMs + offsetSeconds * 1000).toISOString();
  }

  // Mirrors relTime() in the cloud app.
  function rel(isoStr) {
    var s = Math.round((Date.now() - new Date(isoStr).getTime()) / 1000);
    if (s < 60) return s + 's ago';
    var m = Math.round(s / 60);
    if (m < 60) return m + 'm ago';
    var h = Math.round(m / 60);
    if (h < 24) return h + 'h ago';
    return Math.round(h / 24) + 'd ago';
  }

  function stamp(isoStr) {
    return isoStr + ' (' + rel(isoStr) + ')';
  }

  // ---- the sample report --------------------------------------------------
  function report() {
    var b = base();
    var die = iso(b, 0); // 14:22:07 · die, exit 137
    var oom = iso(b, 0); // 14:22:07 · oom
    var opened = iso(b, 2); // 14:22:09
    var probed = iso(b, 4); // 14:22:11
    var resolved = iso(b, 273); // 14:26:40

    return [
      "I'm experiencing the following issue with my Homelab stack:",
      '',
      '# DockTail incident report',
      '',
      '> Exported from DockTail Cloud for analysis. DockTail watches each Docker',
      '> service from up to three vantages — **local** (agent → container),',
      "> **tailnet** (the Tailscale control plane's view of the service) and",
      '> **public** (Funnel probe) — and reports the layer that broke (container /',
      '> exposure / host) plus the strongest observed signal. All timestamps are',
      '> ISO-8601. Use this to understand the failure and suggest what to check next.',
      '',
      '**Service down** — Container was OOM-killed by Docker (exit 137).',
      '',
      '## Summary',
      '',
      '- **Verdict:** Service down (incident kind `service_down`)',
      '- **Severity:** critical',
      '- **Status:** resolved · lasted 4m',
      '- **Affected:** service `grafana` on host `prod-01`',
      '- **What we observed:** Container was OOM-killed by Docker (exit 137).',
      '- **Signal:** oom',
      '',
      '| Field | Value |',
      '| --- | --- |',
      '| Incident ID | `9f3c1a7e-182b-4d55-9a0c-3e7c4b21d8f6` |',
      '| Opened | ' + stamp(opened) + ' |',
      '| Resolved | ' + stamp(resolved) + ' |',
      '| Duration | 4m |',
      '| Exit code (at open) | 137 (SIGKILL) |',
      '| Restarts (at open) | 3 |',
      '',
      '## Where it failed — vantage matrix',
      '',
      'The status of each vantage as captured when the incident opened, alongside the',
      'current live status. This combination is what classifies the fault.',
      '',
      '| Vantage | At open | Live now |',
      '| --- | --- | --- |',
      '| local · agent → container | down | up |',
      '| tailnet · control plane | up | up |',
      '| public · Funnel probe | not configured | not configured |',
      '',
      'Local probe down. Docker reported an OOM kill.',
      '',
      '## Container facts',
      '',
      'Live values for the service right now (only the exit/restart in the summary are',
      'captured at the moment the incident opened).',
      '',
      '| Field | Value |',
      '| --- | --- |',
      '| Image | grafana/grafana:11.3.0 |',
      '| Compose project | observability |',
      '| Service FQDN | grafana.tailnet-1a2b.ts.net |',
      '| Container state (live) | running |',
      '| Docker health (live) | healthy |',
      '| Restarts (live) | 4 |',
      '| CPU (live) | 6.20% |',
      '| Memory (live) | 412 MiB |',
      '| Memory limit | 512 MiB |',
      '| Public Funnel | disabled |',
      '',
      '## Host context',
      '',
      'The Docker host this service runs on.',
      '',
      '| Field | Value |',
      '| --- | --- |',
      '| Host | prod-01 |',
      '| Reporting | online |',
      '| Last seen | ' + stamp(new Date(Date.now() - 9000).toISOString()) + ' |',
      '| Agent version | 1.9.0 |',
      '| Docker version | 27.3.1 |',
      '| Tailscale version | 1.76.6 |',
      '',
      '## Docker signals (incident window)',
      '',
      'Raw Docker failure/lifecycle events the agent forwarded around this incident.',
      '',
      '| Time | Event | Exit | Health | Message |',
      '| --- | --- | --- | --- | --- |',
      '| ' + stamp(oom) + ' | oom | — | — | container killed by the OOM killer |',
      '| ' + stamp(die) + ' | die | 137 | — | container exited |',
      '',
      '## Log tail (captured at incident open)',
      '',
      'The last container log lines captured when the incident opened.',
      '',
      'Captured ' + stamp(opened) + ' · 236 bytes',
      '',
      '```text',
      '14:22:04 lvl=eror msg="query failed" ds=prometheus',
      '14:22:05 lvl=info msg="alerting refresh" auth=[redacted]',
      '14:22:06 fatal: runtime: out of memory',
      '14:22:06   goroutine stack exceeds 1000000000-byte limit',
      '```',
      '',
      '## Health changes',
      '',
      'Status and classification are durable change events; each state remains',
      'effective until the next row. Identical probes are not stored.',
      '',
      '### local · agent → container — 2 recorded states',
      '',
      '| Time | Status | Classification |',
      '| --- | --- | --- |',
      '| ' + stamp(probed) + ' | down | refused |',
      '| ' + stamp(resolved) + ' | up | — |',
      '',
      '### tailnet · control plane — 1 recorded state',
      '',
      '| Time | Status | Classification |',
      '| --- | --- | --- |',
      '| ' + stamp(probed) + ' | up | — |',
      '',
      '## Timeline',
      '',
      'Authoritative health-state changes, incident lifecycle, and Docker signals.',
      'Each recorded health state remains effective until the next change.',
      '',
      '- `' + oom + '` — Docker oom (container killed by the OOM killer)',
      '- `' + die + '` — Docker die (exit 137 · container exited)',
      '- `' + opened + '` — Incident opened — Service down (Container was OOM-killed by Docker (exit 137).)',
      '- `' + probed + '` — local down (refused)',
      '- `' + probed + '` — tailnet up',
      '- `' + resolved + '` — local up',
      '- `' + resolved + '` — Resolved (after 4m)',
      '',
    ].join('\n');
  }

  // ---- the button ---------------------------------------------------------
  function init() {
    var btn = document.getElementById('copy-evidence');
    if (!btn) return;
    var tip = document.getElementById('copy-evidence-tip');
    var timer = null;

    btn.addEventListener('click', function () {
      // The press animation restarts on every click, copy or not.
      btn.classList.remove('press');
      void btn.offsetWidth;
      btn.classList.add('press');

      var done = function () {
        btn.classList.add('is-copied');
        if (tip) {
          // Set the text on copy (not in the markup) so the live region
          // actually announces it.
          tip.textContent = 'Copied';
          tip.classList.add('show');
        }
        clearTimeout(timer);
        timer = setTimeout(function () {
          btn.classList.remove('is-copied');
          if (tip) {
            tip.classList.remove('show');
            tip.textContent = '';
          }
        }, COPIED_MS);
      };

      var text = report();
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(done, function () {
          fallbackCopy(text) && done();
        });
      } else if (fallbackCopy(text)) {
        done();
      }
    });
  }

  // execCommand path for non-secure contexts, where navigator.clipboard is absent.
  function fallbackCopy(text) {
    var ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    var ok = false;
    try {
      ok = document.execCommand('copy');
    } catch (e) {
      ok = false;
    }
    document.body.removeChild(ta);
    return ok;
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
