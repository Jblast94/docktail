package proto

import (
	"encoding/json"
	"fmt"
)

// MessageType is the discriminator on an [Envelope].
type MessageType string

// Agent -> Cloud message types.
const (
	TypeHello        MessageType = "hello"         // first frame after connect
	TypeSnapshot     MessageType = "snapshot"      // full state after each reconcile
	TypeEvent        MessageType = "event"         // a single docker failure signal
	TypeCheckResults MessageType = "check_results" // batched local-vantage probes
	TypeLogExcerpt   MessageType = "log_excerpt"   // last N lines on incident (on by default)
	TypeHeartbeat    MessageType = "heartbeat"     // liveness, every HeartbeatInterval
)

// Cloud -> Agent message types.
const (
	TypeHelloAck MessageType = "hello_ack" // accept/reject + assigned host id
	TypeConfig   MessageType = "config"    // check config + log opt-in flags
)

// Envelope wraps every frame on the wire. Payload is the JSON of the concrete
// message identified by Type.
type Envelope struct {
	Type    MessageType     `json:"type"`
	TS      int64           `json:"ts"` // unix milliseconds the frame was produced
	Payload json.RawMessage `json:"payload"`
}

// Encode marshals msg into an Envelope of the given type at time tsMillis.
func Encode(t MessageType, tsMillis int64, msg any) ([]byte, error) {
	p, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("proto: marshal payload %s: %w", t, err)
	}
	return json.Marshal(Envelope{Type: t, TS: tsMillis, Payload: p})
}

// Decode unmarshals dst from an Envelope's payload.
func (e Envelope) Decode(dst any) error {
	if err := json.Unmarshal(e.Payload, dst); err != nil {
		return fmt.Errorf("proto: unmarshal %s payload: %w", e.Type, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Agent -> Cloud
// ---------------------------------------------------------------------------

// Hello is the first frame. Identity is the Docker engine ID fingerprint; the
// Tailscale node ID is a mutable attribute (can lag at boot) and the hostname
// is display-only.
type Hello struct {
	ProtocolVersion  int      `json:"protocol_version"`
	Fingerprint      string   `json:"fingerprint"`                 // docker engine ID — the host identity & billable unit
	TailscaleNodeID  string   `json:"tailscale_node_id,omitempty"` // mutable attribute, may be empty at boot
	Hostname         string   `json:"hostname,omitempty"`          // display-only, may collide
	AgentVersion     string   `json:"agent_version,omitempty"`
	DockerVersion    string   `json:"docker_version,omitempty"`
	TailscaleVersion string   `json:"tailscale_version,omitempty"`
	Capabilities     []string `json:"capabilities,omitempty"` // e.g. ["http_checks","log_capture"]

	// Static host specs, read once from `docker info` at agent start (display-only).
	OS            string `json:"os,omitempty"`              // docker Info.OperatingSystem, e.g. "Ubuntu 22.04.3 LTS"
	KernelVersion string `json:"kernel_version,omitempty"`  // docker Info.KernelVersion
	Arch          string `json:"arch,omitempty"`            // docker Info.Architecture, e.g. "x86_64"/"aarch64"
	CPUCores      int    `json:"cpu_cores,omitempty"`       // docker Info.NCPU
	MemTotalBytes int64  `json:"mem_total_bytes,omitempty"` // docker Info.MemTotal
}

// Snapshot is the set of monitored services the agent sees. The agent is
// stateless; the cloud diffs snapshots against the catalog to detect
// added/removed/changed services.
//
// Full distinguishes an authoritative snapshot from a partial refresh. A full
// snapshot (the agent's self-discovery, which includes stopped containers) is
// the source of truth for presence: the cloud upserts the listed services and
// treats any catalogued service absent from it as removed. A partial snapshot
// (e.g. the post-reconcile refresh, which only sees running services) upserts
// for enrichment but never drives removals, so a stopped container is not
// mistaken for a deleted one.
type Snapshot struct {
	Services []Service `json:"services"`
	Full     bool      `json:"full"`
}

// Service is one docktail-labeled service as the agent sees it. Mirrors the
// reconciler's view plus runtime status.
type Service struct {
	// Identity (stable within a host).
	Key           string `json:"key"`          // stable key within host (service name, else container name)
	ServiceName   string `json:"service_name"` // tailscale service name (e.g. "svc:myapp")
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name"`
	Image         string `json:"image"`
	ImageTag      string `json:"image_tag,omitempty"`

	// Compose grouping.
	ComposeProject string `json:"compose_project,omitempty"`
	ComposeService string `json:"compose_service,omitempty"`

	// Network / exposure.
	FQDN         string   `json:"fqdn,omitempty"` // tailnet FQDN (e.g. myapp.tailnet.ts.net)
	IPAddress    string   `json:"ip_address,omitempty"`
	Port         string   `json:"port,omitempty"`             // tailscale service port
	TargetPort   string   `json:"target_port,omitempty"`      // container/host port behind it
	ServiceProto string   `json:"service_protocol,omitempty"` // protocol tailscale serves (https/http/tcp)
	Protocol     string   `json:"protocol,omitempty"`         // protocol the container speaks
	Tags         []string `json:"tags,omitempty"`
	Networks     []string `json:"networks,omitempty"`

	// Funnel (public internet exposure).
	FunnelEnabled  bool   `json:"funnel_enabled"`
	FunnelPort     string `json:"funnel_port,omitempty"`
	FunnelProtocol string `json:"funnel_protocol,omitempty"`
	FunnelPath     string `json:"funnel_path,omitempty"`

	// Runtime status from docker.
	State        string `json:"state"`                   // running/exited/restarting/paused/created
	DockerHealth string `json:"docker_health,omitempty"` // healthy/unhealthy/starting (if container has a healthcheck)
	RestartCount int    `json:"restart_count,omitempty"`

	// Live resource usage from `docker stats` (current value, sampled per
	// snapshot). CPUPercent is a pointer so a genuine 0% (an idle container) is
	// distinct from "not sampled" (nil — e.g. the container isn't running, or the
	// first sample, which has no prior reading to delta against). Memory is zero
	// only when unsampled, since a running container's working set is never 0.
	CPUPercent    *float64 `json:"cpu_percent,omitempty"`     // container CPU usage as % of all host cores
	MemUsageBytes int64    `json:"mem_usage_bytes,omitempty"` // working set (usage minus inactive file cache)
	MemLimitBytes int64    `json:"mem_limit_bytes,omitempty"` // effective limit (container limit, else host total)
}

// Event is a single docker-side failure signal. The kind is the product —
// "why", not just "down".
type Event struct {
	Kind          EventKind `json:"kind"`
	ServiceKey    string    `json:"service_key,omitempty"`
	ContainerID   string    `json:"container_id"`
	ContainerName string    `json:"container_name,omitempty"`
	ExitCode      *int      `json:"exit_code,omitempty"`     // for die
	RestartCount  int       `json:"restart_count,omitempty"` // for restart_loop
	HealthStatus  string    `json:"health_status,omitempty"` // for health_status
	Message       string    `json:"message,omitempty"`
	OccurredAt    int64     `json:"occurred_at"` // unix ms
}

// EventKind enumerates the docker failure signals the agent forwards.
type EventKind string

const (
	EventDie          EventKind = "die"           // container exited (carries exit code)
	EventOOM          EventKind = "oom"           // out-of-memory kill
	EventHealthStatus EventKind = "health_status" // docker healthcheck transition
	EventRestartLoop  EventKind = "restart_loop"  // crash-looping
	EventStart        EventKind = "start"
	EventStop         EventKind = "stop"
)

// CheckResults is a batch of local-vantage probe results.
type CheckResults struct {
	Results []CheckResult `json:"results"`
}

// CheckResult is one probe outcome. Vantage is always "local" from the agent;
// the prober contributes "tailnet" and the public probe contributes "public"
// on the cloud side.
type CheckResult struct {
	ServiceKey string `json:"service_key"`
	Vantage    string `json:"vantage"` // VantageLocal from the agent
	Kind       string `json:"kind"`    // tcp/http
	OK         bool   `json:"ok"`
	LatencyMS  int64  `json:"latency_ms"`
	StatusCode int    `json:"status_code,omitempty"` // http
	Class      string `json:"class,omitempty"`       // failure classification (see Classification constants)
	Error      string `json:"error,omitempty"`
	CheckedAt  int64  `json:"checked_at"` // unix ms
}

// LogExcerpt carries the last N lines of a service's logs on incident. Capture
// is on by default and gated by LogConfig; capped at MaxLogLines / MaxLogBytes.
type LogExcerpt struct {
	ServiceKey  string   `json:"service_key"`
	ContainerID string   `json:"container_id"`
	Lines       []string `json:"lines"`
	ByteSize    int      `json:"byte_size"`
	CapturedAt  int64    `json:"captured_at"` // unix ms
}

// Heartbeat is emitted every HeartbeatInterval and doubles as liveness.
type Heartbeat struct {
	Uptime int64 `json:"uptime_seconds,omitempty"`
}

// ---------------------------------------------------------------------------
// Cloud -> Agent
// ---------------------------------------------------------------------------

// HelloAck is the cloud's response to [Hello].
type HelloAck struct {
	Accepted      bool       `json:"accepted"`
	HostID        string     `json:"host_id,omitempty"` // server-assigned host UUID
	Reason        RejectCode `json:"reason,omitempty"`  // set when Accepted is false
	ConfigVersion int        `json:"config_version"`    // current config version for this workspace
	ServerTime    int64      `json:"server_time"`       // unix ms, for clock-skew awareness
}

// RejectCode explains a rejected [Hello].
type RejectCode string

const (
	RejectInvalidKey       RejectCode = "invalid_key"
	RejectBlocked          RejectCode = "blocked"            // host row is blocked (per-host revocation)
	RejectDuplicate        RejectCode = "duplicate_identity" // another live connection claims this fingerprint
	RejectProtocolMismatch RejectCode = "protocol_mismatch"
	RejectOverCap          RejectCode = "over_cap" // workspace past host cap grace window
)

// Config pushes per-service check configuration and log-capture settings to the
// agent. There are deliberately no other knobs: no exec, no deploy, no shell.
type Config struct {
	Version int           `json:"version"`
	Checks  []CheckConfig `json:"checks"`
	Logs    LogConfig     `json:"logs"`

	// Unmonitored marks a host the cloud accepts but does not monitor (the
	// workspace is past its plan host cap). A monitored host omits the field
	// (absent ⇒ monitored, so an old cloud or old agent behaves as before). An
	// agent that sees Unmonitored should throttle to occasional catalog-teaser
	// snapshots plus heartbeats and stop emitting checks/events/log excerpts;
	// the cloud also drops those frames on its side. Checks is empty in an
	// unmonitored config.
	Unmonitored bool `json:"unmonitored,omitempty"`

	// Deprecated: superseded by Logs. Retained for wire compatibility with
	// older agents that only read log_opt_in; new agents prefer Logs when
	// Logs.Mode is set. The cloud no longer populates this field.
	LogOptIn []string `json:"log_opt_in,omitempty"`
}

// LogConfig controls incident log capture. Mode is the workspace default and
// Overrides set a per-service mode; the effective mode for a service is
// Overrides[serviceKey] when present, else Mode (an empty Mode means the default
// LogModeIncident). Continuous capture is reserved and not yet emitted.
type LogConfig struct {
	Mode      string            `json:"mode"`                // LogMode*; "" ⇒ LogModeIncident
	Overrides map[string]string `json:"overrides,omitempty"` // service_key -> LogMode*
}

// CheckConfig configures a single local-vantage check.
type CheckConfig struct {
	ServiceKey   string `json:"service_key"`
	Kind         string `json:"kind"`             // tcp/http
	Target       string `json:"target,omitempty"` // host:port override; default derived from snapshot
	Path         string `json:"path,omitempty"`   // http path
	ExpectStatus int    `json:"expect_status,omitempty"`
	IntervalMS   int64  `json:"interval_ms"`
}

// ---------------------------------------------------------------------------
// Shared vocabulary
// ---------------------------------------------------------------------------

// Vantages — the three points a service is observed from.
const (
	VantageLocal   = "local"   // agent -> container IP
	VantageTailnet = "tailnet" // prober -> service FQDN over WireGuard
	VantagePublic  = "public"  // plain HTTP probe for Funnel services
)

// Probe failure classifications. The classification is the product: "why", not
// "down".
const (
	ClassDNS        = "dns"
	ClassTimeout    = "timeout"
	ClassRefused    = "refused"
	ClassTLS        = "tls"
	ClassHTTP5xx    = "http_5xx"
	ClassACLBlocked = "acl_blocked"
	ClassContainer  = "container" // local down -> container problem
)

// Caps for log capture (also enforced agent-side).
const (
	MaxLogLines = 40
	MaxLogBytes = 8 * 1024
)

// Log capture modes — the workspace default ([LogConfig.Mode]) and per-service
// overrides ([LogConfig.Overrides]) both use these.
const (
	LogModeIncident   = "incident"   // capture the tail on a down-signal event (default)
	LogModeOff        = "off"        // never capture
	LogModeContinuous = "continuous" // reserved: rolling capture, not yet implemented
)
