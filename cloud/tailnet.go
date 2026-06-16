package cloud

import (
	"context"
	"fmt"
	"time"

	"github.com/marvinvr/docktail/cloud/proto"
	"github.com/marvinvr/docktail/tailscale"
)

// tailnetSource is the read-only view of the local tailscaled the collector needs
// to source the tailnet vantage from the host's OWN netmap — no API key, no
// stored credentials. *tailscale.Client satisfies it. Nil when DockTail has no
// tailscale client, in which case the collector simply omits the tailnet vantage.
type tailnetSource interface {
	// GetCurrentServices returns the tailscale services currently published via
	// `tailscale serve`, keyed by "svc:<name>:<port>".
	GetCurrentServices(ctx context.Context) (map[string]tailscale.ServiceEndpoint, error)
	// Status returns this node's stable ID and the liveness of the tailnet peers
	// it can see, from `tailscale status`.
	Status(ctx context.Context) (*tailscale.TailnetStatus, error)
}

// tailnetServeKey is the `tailscale serve` key for a service: "svc:<name>:<port>".
// Empty when the service has no tailscale service name/port (e.g. a funnel-only
// service), in which case it has no tailnet-serve vantage.
func tailnetServeKey(svc proto.Service) string {
	if svc.ServiceName == "" || svc.Port == "" {
		return ""
	}
	return fmt.Sprintf("svc:%s:%s", svc.ServiceName, svc.Port)
}

// tailnetResults builds tailnet-vantage check results from the host's
// `tailscale serve` config: OK ⇒ the service is published on the tailnet, !OK
// (class serve) ⇒ it is up locally but not published. Returns nil when there is
// no tailnet source or the serve config can't be read (no tailnet) — the cloud
// then leaves the tailnet vantage not_configured and degrades to local + docker
// signals. Only services with a tailscale service name/port get a result.
func (c *Collector) tailnetResults(ctx context.Context, services []proto.Service) []proto.CheckResult {
	if c.tailnet == nil {
		return nil
	}
	served, err := c.tailnet.GetCurrentServices(ctx)
	if err != nil {
		c.log.Debug().Err(err).Msg("cloud: tailnet serve status unavailable; skipping tailnet vantage")
		return nil
	}
	now := nowMillis()
	out := make([]proto.CheckResult, 0, len(services))
	for _, svc := range services {
		key := tailnetServeKey(svc)
		if key == "" {
			continue
		}
		_, published := served[key]
		res := proto.CheckResult{
			ServiceKey: svc.Key,
			Vantage:    proto.VantageTailnet,
			Kind:       "serve",
			OK:         published,
			CheckedAt:  now,
		}
		if !published {
			res.Class = proto.ClassServe
		}
		out = append(out, res)
	}
	return out
}

// tailnetSelfID reads this node's tailscale StableNodeID (best-effort, bounded)
// for the hello frame. Empty when there is no tailnet — the cloud then can't
// split THIS host's outages into agent_down vs host_down (falls back to
// host_down).
func (c *Collector) tailnetSelfID(ctx context.Context) string {
	if c.tailnet == nil {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	st, err := c.tailnet.Status(cctx)
	if err != nil || st == nil {
		return ""
	}
	return st.SelfNodeID
}

// tailnetLoop reports the host's local-netmap peer liveness on the heartbeat
// cadence. It is the signal the cloud uses to tell a dead agent (the device is
// still online) from a dead host (device gone) for OTHER hosts on the tailnet.
// Skipped while unmonitored (the cloud drops the frame) and silent when there is
// no tailnet.
func (c *Collector) tailnetLoop(ctx context.Context, conn *wsConn) {
	ticker := time.NewTicker(proto.HeartbeatInterval * time.Second)
	defer ticker.Stop()
	c.sampleAndSendTailnet(ctx, conn)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sampleAndSendTailnet(ctx, conn)
		}
	}
}

func (c *Collector) sampleAndSendTailnet(ctx context.Context, conn *wsConn) {
	if c.tailnet == nil {
		return
	}
	c.mu.RLock()
	unmonitored := c.unmonitored
	c.mu.RUnlock()
	if unmonitored {
		return
	}
	st, err := c.tailnet.Status(ctx)
	if err != nil || st == nil {
		return // no tailnet → nothing to report
	}
	peers := make([]proto.TailnetPeer, 0, len(st.Peers))
	for _, p := range st.Peers {
		if p.NodeID == "" {
			continue
		}
		tp := proto.TailnetPeer{
			NodeID:   p.NodeID,
			Hostname: p.Hostname,
			Online:   p.Online,
		}
		if !p.Online && !p.LastSeen.IsZero() {
			tp.LastSeen = p.LastSeen.UnixMilli()
		}
		peers = append(peers, tp)
	}
	if len(peers) == 0 {
		return
	}
	c.send(conn, proto.TypeTailnet, proto.TailnetReport{Peers: peers})
}
