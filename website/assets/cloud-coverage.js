/**
 * <cloud-coverage> — the shared DockTail Cloud coverage diagram.
 *
 * Rendered on the homepage cloud section and in the /cloud hero. It is a
 * diagram, not a product screenshot: every label is a signal the agent really
 * reports (see cloud/proto/messages.go, cloud/checks.go, cloud/hostmetrics.go).
 *
 *   <cloud-coverage variant="compact"></cloud-coverage>
 *   <cloud-coverage variant="full"></cloud-coverage>
 *
 * variant  compact = chain + verdict, full = chain + verdict + fleet + legend
 *
 * Light DOM with its own scoped stylesheet, so it does not depend on the
 * Tailwind CDN generating classes for markup inserted after first paint.
 */
(function () {
  'use strict';

  var STYLE_ID = 'dtc-coverage-styles';

  var CSS = [
    'cloud-coverage{display:block}',
    '.dtc{position:relative;border:1px solid #e5e7eb;border-radius:.5rem;padding:1.5rem 1.25rem 1.25rem;',
      'font-size:12px;line-height:1.5;color:#111827;text-align:left}',
    '.dtc{background:#fff;box-shadow:0 20px 60px rgba(17,24,39,.10)}',

    '.dtc-sr{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}',

    '.dtc-head{display:flex;flex-wrap:wrap;align-items:baseline;justify-content:space-between;',
      'gap:.5rem 1rem;margin-bottom:1rem}',
    '.dtc-head-title{font-size:13px;font-weight:600;color:#111827}',

    '.dtc-hostbar{display:flex;align-items:center;gap:.5rem;margin-bottom:.5rem;',
      'font-size:10px;color:#6b7280}',
    '.dtc-hostbar b{font-weight:600;color:#374151}',
    '.dtc-live{display:inline-flex;align-items:center;gap:.35rem;margin-left:auto;color:#9ca3af}',
    '.dtc-live i{width:5px;height:5px;border-radius:9999px;background:#22c55e;animation:dtcPulse 2.4s ease-in-out infinite}',

    /* chain ------------------------------------------------------------- */
    '.dtc-chain{display:flex;align-items:stretch;margin:0;padding:0;list-style:none}',
    '.dtc-hop{flex:1 1 0;min-width:0;display:flex;flex-direction:column;gap:.3rem;',
      'padding:.55rem .6rem;border:1px solid #e5e7eb;border-radius:.375rem;background:#fff}',
    '.dtc-hop-name{font-size:10px;letter-spacing:.08em;text-transform:uppercase;color:#9ca3af}',
    '.dtc-hop-status{display:flex;align-items:center;gap:.35rem;font-size:11px;color:#374151;',
      'white-space:nowrap;overflow:hidden;text-overflow:ellipsis}',
    '.dtc-hop-signal{font-size:9px;line-height:1.35;color:#9ca3af;overflow-wrap:anywhere}',
    '.dtc-dot{flex:0 0 auto;width:6px;height:6px;border-radius:9999px;background:#d1d5db}',

    '.dtc-hop[data-state="ok"]{border-color:#bbf7d0;background:#f0fdf4}',
    '.dtc-hop[data-state="ok"] .dtc-hop-status{color:#15803d}',
    '.dtc-hop[data-state="ok"] .dtc-dot{background:#22c55e}',
    '.dtc-hop[data-state="down"]{border-color:#fecaca;background:#fef2f2}',
    '.dtc-hop[data-state="down"] .dtc-hop-status{color:#b91c1c}',
    '.dtc-hop[data-state="down"] .dtc-dot{background:#ef4444;animation:dtcPulse 1.8s ease-in-out infinite}',

    '.dtc-arrow{flex:0 0 auto;width:1.1rem;display:flex;align-items:center;justify-content:center;color:#d1d5db}',
    '.dtc-arrow svg{width:12px;height:12px;animation:dtcFlow 2.4s ease-in-out infinite;',
      'animation-delay:calc(var(--i,0) * .18s)}',

    /* verdict ----------------------------------------------------------- */
    '.dtc-verdict{position:relative;margin-top:1.1rem;padding:.7rem .8rem;border:1px solid #fde68a;',
      'border-left-width:3px;border-radius:.375rem;background:#fffbeb}',
    '.dtc-verdict::before{content:"";position:absolute;top:-5px;left:34%;width:8px;height:8px;',
      'border-left:1px solid #fde68a;border-top:1px solid #fde68a;background:#fffbeb;transform:rotate(45deg)}',
    '.dtc-verdict-label{font-size:9px;letter-spacing:.12em;text-transform:uppercase;color:#b45309;margin-bottom:.3rem}',
    '.dtc-verdict-title{font-size:12px;font-weight:600;color:#111827;margin-bottom:.25rem}',
    '.dtc-verdict-body{font-size:11px;color:#4b5563;line-height:1.55}',
    '.dtc-verdict-body b{font-weight:600;color:#111827}',

    /* fleet -------------------------------------------------------------- */
    '.dtc-fleet{margin-top:1rem;border-top:1px dashed #e5e7eb;padding-top:.85rem;',
      'display:flex;flex-direction:column;gap:.5rem}',
    '.dtc-row{display:flex;align-items:center;gap:.7rem;font-size:10px;color:#6b7280}',
    '.dtc-row-host{flex:0 0 5.5rem;color:#374151}',
    '.dtc-mini{display:inline-flex;gap:3px;flex:0 0 auto}',
    '.dtc-mini i{width:6px;height:6px;border-radius:9999px;background:#22c55e}',
    '.dtc-mini i[data-state="off"]{background:#e5e7eb}',
    '.dtc-row-svc{flex:1 1 auto;min-width:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;color:#9ca3af}',
    '.dtc-row-state{flex:0 0 auto;color:#15803d}',

    '.dtc-legend{display:flex;flex-wrap:wrap;gap:.4rem .9rem;margin-top:.85rem;font-size:9px;color:#9ca3af}',
    '.dtc-legend span{display:inline-flex;align-items:center;gap:.35rem}',
    '.dtc-legend i{width:6px;height:6px;border-radius:9999px}',

    '.dtc-foot{margin-top:1rem;border-top:1px solid #e5e7eb;padding-top:.7rem;',
      'font-size:11px;color:#4b5563}',
    '.dtc-foot-label{margin-right:.6rem;font-size:9px;letter-spacing:.12em;',
      'text-transform:uppercase;color:#9ca3af}',

    /* stacked layout ------------------------------------------------------ */
    '@media (max-width:767px){',
      '.dtc-chain{flex-direction:column}',
      '.dtc-hop{flex-direction:row;align-items:center;gap:.6rem}',
      '.dtc-hop-name{flex:0 0 4.75rem}',
      '.dtc-hop-status{flex:0 0 auto}',
      '.dtc-hop-signal{flex:1 1 auto;text-align:right;line-height:1.5}',
      '.dtc-arrow{width:auto;height:.9rem;transform:rotate(90deg)}',
      '.dtc-verdict::before{left:1.5rem}',
      '.dtc-row-host{flex-basis:4.5rem}',
      '.dtc-row-svc{display:none}',
    '}',

    '@keyframes dtcPulse{0%,100%{opacity:.45}50%{opacity:1}}',
    '@keyframes dtcFlow{0%,100%{opacity:.35}50%{opacity:1}}',
    '@media (prefers-reduced-motion:reduce){',
      '.dtc-dot,.dtc-arrow svg,.dtc-live i{animation:none!important;opacity:1!important}',
    '}'
  ].join('');

  /* The failing service: OOM-killed container whose Tailscale service is still
     advertised — the case a plain uptime check cannot tell apart. */
  var HOPS = [
    { name: 'host',      state: 'ok',   status: 'up',         signal: 'heartbeat · vitals' },
    { name: 'container', state: 'down', status: 'oom killed', signal: 'docker event' },
    { name: 'local',     state: 'down', status: 'refused',    signal: 'tcp check :3000' },
    { name: 'tailnet',   state: 'ok',   status: 'approved',   signal: 'control plane' }
  ];

  var FLEET = [
    { host: 'db-01',   dots: ['ok','ok','ok','off'], services: 'postgres, redis, +4 services' },
    { host: 'edge-01', dots: ['ok','ok','ok','ok'], services: 'nginx, traefik, +2 services' }
  ];

  var CHEVRON = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" aria-hidden="true"><path d="M9 5l7 7-7 7"/></svg>';

  function injectStyles() {
    if (document.getElementById(STYLE_ID)) return;
    var el = document.createElement('style');
    el.id = STYLE_ID;
    el.textContent = CSS;
    document.head.appendChild(el);
  }

  function hopMarkup(hop) {
    return '<li class="dtc-hop" data-state="' + hop.state + '">' +
        '<span class="dtc-hop-name">' + hop.name + '</span>' +
        '<span class="dtc-hop-status"><i class="dtc-dot"></i>' + hop.status + '</span>' +
        '<span class="dtc-hop-signal">' + hop.signal + '</span>' +
      '</li>';
  }

  function chainMarkup() {
    var parts = [];
    HOPS.forEach(function (hop, i) {
      if (i > 0) {
        parts.push('<li class="dtc-arrow" aria-hidden="true" style="--i:' + i + '">' + CHEVRON + '</li>');
      }
      parts.push(hopMarkup(hop));
    });
    return '<ul class="dtc-chain">' + parts.join('') + '</ul>';
  }

  function fleetMarkup() {
    return '<div class="dtc-fleet">' + FLEET.map(function (row) {
      var dots = row.dots.map(function (s) { return '<i data-state="' + s + '"></i>'; }).join('');
      return '<div class="dtc-row">' +
          '<span class="dtc-row-host">' + row.host + '</span>' +
          '<span class="dtc-mini" aria-hidden="true">' + dots + '</span>' +
          '<span class="dtc-row-svc">' + row.services + '</span>' +
          '<span class="dtc-row-state">all clear</span>' +
        '</div>';
    }).join('') + '</div>';
  }

  function legendMarkup() {
    return '<div class="dtc-legend">' +
        '<span><i style="background:#22c55e"></i>watched, healthy</span>' +
        '<span><i style="background:#ef4444"></i>problem, with evidence</span>' +
      '</div>';
  }

  function render(el) {
    var variant = el.getAttribute('variant') === 'compact' ? 'compact' : 'full';

    var html = '<div class="dtc" data-variant="' + variant + '" role="group" aria-labelledby="dtc-sr-' + variant + '">' +
      '<p class="dtc-sr" id="dtc-sr-' + variant + '">Diagram: DockTail Cloud watches every hop of a published service — host, container, local reachability, and tailnet exposure — across every host in the fleet.</p>' +

      '<div class="dtc-head">' +
        '<span class="dtc-head-title">Every host, every service, monitored. Zero config.</span>' +
      '</div>' +

      '<div class="dtc-hostbar"><b>prod-01</b><span>/</span><span>grafana</span>' +
        '<span class="dtc-live"><i></i>checked every 30s</span></div>' +

      chainMarkup() +

      '<div class="dtc-verdict">' +
        '<div class="dtc-verdict-label">Verdict · container problem</div>' +
        '<div class="dtc-verdict-title">grafana broke, not its exposure.</div>' +
        '<p class="dtc-verdict-body">OOM-killed by Docker, <b>exit 137</b>. The tailnet still advertises the service, so re-publishing it changes nothing. <b>40 log lines</b> captured at the moment of failure.</p>' +
      '</div>' +

      (variant === 'full' ? fleetMarkup() + legendMarkup() : '') +

      '<div class="dtc-foot"><span class="dtc-foot-label">watching</span>'  +
        '3 hosts · 28 services · 35 containers</div>' +
    '</div>';

    el.innerHTML = html;
  }

  function define() {
    injectStyles();
    if (window.customElements && !window.customElements.get('cloud-coverage')) {
      window.customElements.define('cloud-coverage', class extends HTMLElement {
        connectedCallback() { render(this); }
      });
      return;
    }
    // Fallback for browsers without custom elements.
    Array.prototype.forEach.call(document.querySelectorAll('cloud-coverage'), render);
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', define);
  } else {
    define();
  }
})();
