// Package proto defines the wire contract between the DockTail agent (this repo,
// AGPL) and DockTail Cloud's proprietary agent-plane.
//
// It is intentionally dependency-free (stdlib only) so it stays a clean
// licensing firewall: the AGPL agent and the proprietary cloud both import an
// identical, neutral set of types without either contaminating the other.
//
// NOTE: this is currently a verbatim copy of docktail-cloud/proto, kept in sync
// by hand. The intended end-state (see that repo's PLAN.md) is a single shared
// Apache-2.0 module (github.com/marvinvr/docktail-proto) that both sides import;
// until that module exists, the two copies MUST be kept byte-identical on the
// wire (same JSON field names and message shapes).
//
// Transport: outbound-only WSS, JSON messages, 30s heartbeat, jittered
// reconnect. The protocol is metadata-only — there are no exec, deploy, or shell
// message types, by design and verifiable in this open source.
package proto
