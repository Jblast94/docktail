package tailscale

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// TailnetStatus is a minimal view of `tailscale status --json`: this node's
// stable ID plus the online/offline status of the tailnet devices (peers) it can
// see. DockTail Cloud uses it (read over the local daemon, no API key) to report
// peer device liveness so the cloud can tell a dead agent from a dead host.
type TailnetStatus struct {
	SelfNodeID string
	Peers      []TailnetPeerStatus
}

// TailnetPeerStatus is one tailnet device's liveness as seen in this node's netmap.
type TailnetPeerStatus struct {
	NodeID   string
	Hostname string
	Online   bool
	LastSeen time.Time
}

// statusJSON is the subset of `tailscale status --json` (tailscaled's
// ipnstate.Status) that we parse.
type statusJSON struct {
	Self *statusNode            `json:"Self"`
	Peer map[string]*statusNode `json:"Peer"`
}

type statusNode struct {
	ID       string    `json:"ID"`
	HostName string    `json:"HostName"`
	Online   bool      `json:"Online"`
	LastSeen time.Time `json:"LastSeen"`
}

// Status runs `tailscale status --json` and parses this node's stable ID and the
// liveness of the peers it can see. Returns an error if the daemon isn't
// reachable or the output can't be parsed — callers treat that as "no tailnet"
// and skip reporting (the tailnet vantage degrades to not_configured).
func (c *Client) Status(ctx context.Context) (*TailnetStatus, error) {
	cmd := c.tailscaleCmd(ctx, "status", "--json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tailscale status: %w (output: %s)", err, string(output))
	}
	var st statusJSON
	if err := json.Unmarshal([]byte(stripWarnings(output)), &st); err != nil {
		return nil, fmt.Errorf("tailscale status: parse json: %w", err)
	}
	out := &TailnetStatus{}
	if st.Self != nil {
		out.SelfNodeID = st.Self.ID
	}
	for _, p := range st.Peer {
		if p == nil || p.ID == "" {
			continue
		}
		out.Peers = append(out.Peers, TailnetPeerStatus{
			NodeID:   p.ID,
			Hostname: p.HostName,
			Online:   p.Online,
			LastSeen: p.LastSeen,
		})
	}
	return out, nil
}
