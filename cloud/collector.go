package cloud

import (
	"context"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/rs/zerolog"

	"github.com/marvinvr/docktail/cloud/proto"
	"github.com/marvinvr/docktail/docker"
	apptypes "github.com/marvinvr/docktail/types"
)

// agentVersion is reported in Hello. Bump on releases of the cloud module.
const agentVersion = "0.1.0"

// restartLoopThreshold is the container RestartCount above which a die is also
// treated as a restart-loop signal.
const restartLoopThreshold = 3

// unmonitoredSnapshotInterval throttles snapshots for a host the cloud reports
// as unmonitored (past the workspace plan cap). Such a host stops sending
// checks/events/logs entirely and emits only this occasional catalog-teaser
// snapshot plus its heartbeat, until the cloud promotes it again.
const unmonitoredSnapshotInterval = 5 * time.Minute

// Collector is DockTail's cloud reporting module. It implements
// reconciler.Observer: it receives the reconciler's computed services and the
// docker event stream, maps them to wire messages, and streams them over an
// outbound WSS connection it manages itself. It also runs local-vantage checks
// and a heartbeat while connected.
type Collector struct {
	cfg     Config
	docker  *docker.Client
	log     zerolog.Logger
	checker *checker

	fingerprint   string
	hostname      string
	dockerVersion string
	specs         docker.HostSpec

	mu           sync.RWMutex
	conn         *wsConn             // current live connection, or nil when disconnected
	latest       []proto.Service     // last computed snapshot (sent on (re)connect)
	checks       []proto.CheckConfig // cloud-pushed check config
	logMode      string              // workspace default capture mode ("" ⇒ proto.LogModeIncident)
	logOverrides map[string]string   // per-service capture mode override (service key -> proto.LogMode*)
	cfgVer       int
	unmonitored  bool      // cloud reports this host past the plan cap; throttle output
	lastTeaser   time.Time // last throttled teaser snapshot sent while unmonitored

	statsMu sync.Mutex           // guards prevCPU only
	prevCPU map[string]cpuSample // last CPU counters per container, for % deltas
}

// cpuSample is the previous CPU counter reading kept per container. Docker
// reports cumulative counters, so a percentage is the delta between two reads.
type cpuSample struct {
	total  uint64
	system uint64
}

// containerStats is the per-container live usage attached to each wire Service.
// cpuPercent is nil when unknown (not running, or the first sample with no prior
// reading); a non-nil 0 is a genuine idle reading. memUsage/memLimit are zero
// only when unsampled (a running container's working set is never 0).
type containerStats struct {
	cpuPercent *float64
	memUsage   int64
	memLimit   int64
}

// NewCollector builds a Collector, reading the host fingerprint (docker engine
// ID) and versions up front. Returns an error only if the engine ID can't be
// read — without it there is no stable host identity.
func NewCollector(ctx context.Context, cfg Config, dc *docker.Client, logger zerolog.Logger) (*Collector, error) {
	fp, err := dc.EngineID(ctx)
	if err != nil {
		return nil, err
	}
	return &Collector{
		cfg:           cfg,
		docker:        dc,
		log:           logger,
		checker:       newChecker(),
		fingerprint:   fp,
		hostname:      dc.Hostname(ctx),
		dockerVersion: dc.ServerVersion(ctx),
		specs:         dc.HostSpecs(ctx),
		logOverrides:  map[string]string{},
		prevCPU:       map[string]cpuSample{},
	}, nil
}

// Fingerprint is the docker engine ID used as the host identity.
func (c *Collector) Fingerprint() string { return c.fingerprint }

// ---- reconciler.Observer -------------------------------------------------

// OnReconcile receives the reconciler's freshly computed services, enriches them
// with runtime detail, stores them, and (if connected) sends a snapshot.
func (c *Collector) OnReconcile(ctx context.Context, services []*apptypes.ContainerService) {
	built := c.buildServices(ctx, services)

	c.mu.Lock()
	c.latest = built
	conn := c.conn
	unmonitored := c.unmonitored
	c.mu.Unlock()

	// Unmonitored: keep latest fresh for a future promotion but stay off the wire
	// here — the throttled discover loop owns the occasional teaser snapshot.
	if conn != nil && !unmonitored {
		// Partial: the reconciler only sees running services, so this refreshes
		// enrichment (FQDN/funnel) but must not drive removals — otherwise a
		// stopped container would be mistaken for a deleted one.
		c.send(conn, proto.TypeSnapshot, proto.Snapshot{Services: built, Full: false})
		c.log.Debug().Int("services", len(built)).Msg("cloud: snapshot sent")
	}
}

// OnEvent maps a docker event to wire events and (if connected) sends them,
// capturing a log excerpt for opted-in services on down signals.
func (c *Collector) OnEvent(ctx context.Context, msg events.Message) {
	evs := c.mapEvents(ctx, msg)
	if len(evs) == 0 {
		return
	}
	c.mu.RLock()
	conn := c.conn
	unmonitored := c.unmonitored
	c.mu.RUnlock()
	// Unmonitored: the cloud drops events and log excerpts, so don't send them
	// (and skip the log capture work entirely).
	if conn == nil || unmonitored {
		return
	}
	for _, ev := range evs {
		c.send(conn, proto.TypeEvent, ev)
		c.maybeCaptureLogs(ctx, conn, ev)
	}
}

// ---- snapshot building ---------------------------------------------------

// buildServices maps reconciler ContainerService values to wire Services,
// enriching each with a single inspect + stats sample per distinct container.
func (c *Collector) buildServices(ctx context.Context, services []*apptypes.ContainerService) []proto.Service {
	type enriched struct {
		info  docker.CloudInfo
		stats containerStats
	}
	cache := make(map[string]enriched)
	out := make([]proto.Service, 0, len(services))
	for _, cs := range services {
		if cs == nil {
			continue
		}
		e, ok := cache[cs.ContainerID]
		if !ok {
			if ci, err := c.docker.InspectCloud(ctx, cs.ContainerID); err == nil {
				e.info = ci
			}
			e.stats = c.sampleStats(ctx, cs.ContainerID, e.info.State)
			cache[cs.ContainerID] = e
		}
		out = append(out, toService(cs, e.info, e.stats))
	}
	present := make(map[string]struct{}, len(cache))
	for id := range cache {
		present[id] = struct{}{}
	}
	c.pruneStats(present)
	return out
}

// sampleStats reads a one-shot docker stats sample for a running container and
// turns it into a current-value usage reading. Memory is taken as-is; CPU% is
// the delta of cumulative counters against this container's previous sample —
// zero on the first sample, after which it self-corrects. Best-effort: a
// non-running container or any error yields a zero reading, which the cloud
// stores as "unknown" (NULL).
func (c *Collector) sampleStats(ctx context.Context, containerID, state string) containerStats {
	if containerID == "" || state != "running" {
		return containerStats{}
	}
	s, err := c.docker.ContainerStats(ctx, containerID)
	if err != nil {
		c.log.Debug().Err(err).Str("container", containerID).Msg("cloud: stats sample failed")
		return containerStats{}
	}
	out := containerStats{memUsage: s.MemUsageBytes, memLimit: s.MemLimitBytes}

	c.statsMu.Lock()
	prev, ok := c.prevCPU[containerID]
	c.prevCPU[containerID] = cpuSample{total: s.CPUTotalUsage, system: s.CPUSystemUsage}
	c.statsMu.Unlock()

	// A percentage needs two samples. Report it once we have a prior reading and
	// the host system-time advanced — *including* a genuine 0% for an idle
	// container (cpuDelta 0), which is a real value, not "unknown". The formula
	// is self-normalizing on the system-time delta, so a varying interval between
	// samples (reconcile vs. discovery loop) is fine. Skip only when the
	// container's own counter went backwards (a restart reset it); the next tick
	// re-syncs against the fresh prev.
	if ok && s.CPUSystemUsage > prev.system && s.CPUTotalUsage >= prev.total {
		cpuDelta := float64(s.CPUTotalUsage - prev.total)
		sysDelta := float64(s.CPUSystemUsage - prev.system)
		onlineCPUs := float64(s.OnlineCPUs)
		if onlineCPUs == 0 {
			onlineCPUs = 1
		}
		pct := math.Round((cpuDelta/sysDelta)*onlineCPUs*100*100) / 100 // 2 decimals
		out.cpuPercent = &pct
	}
	return out
}

// pruneStats drops previous-CPU samples for containers absent from the latest
// build, keeping the cache bounded to currently-managed containers.
func (c *Collector) pruneStats(present map[string]struct{}) {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	for id := range c.prevCPU {
		if _, ok := present[id]; !ok {
			delete(c.prevCPU, id)
		}
	}
}

// toService maps a reconciler ContainerService + docker enrichment onto the wire
// Service. FQDN is intentionally left empty in v1: the agent does not reliably
// know the tailnet MagicDNS domain, so the cloud prober populates the tailnet
// vantage and FQDN.
func toService(cs *apptypes.ContainerService, info docker.CloudInfo, stats containerStats) proto.Service {
	svc := proto.Service{
		Key:            serviceKeyForContainerService(cs),
		ServiceName:    cs.ServiceName,
		ContainerID:    cs.ContainerID,
		ContainerName:  cs.ContainerName,
		Image:          info.Image,
		ImageTag:       info.ImageTag,
		ComposeProject: info.ComposeProject,
		ComposeService: info.ComposeService,
		IPAddress:      cs.IPAddress,
		Port:           cs.Port,
		TargetPort:     cs.TargetPort,
		ServiceProto:   cs.ServiceProtocol,
		Protocol:       cs.Protocol,
		Tags:           cs.Tags,
		Networks:       info.Networks,
		State:          info.State,
		DockerHealth:   info.Health,
		RestartCount:   info.RestartCount,
		CPUPercent:     stats.cpuPercent,
		MemUsageBytes:  stats.memUsage,
		MemLimitBytes:  stats.memLimit,
	}
	if cs.FunnelEnabled {
		svc.FunnelEnabled = true
		svc.FunnelPort = firstNonEmpty(cs.FunnelFunnelPort, cs.FunnelPort)
		svc.FunnelProtocol = cs.FunnelProtocol
		svc.FunnelPath = cs.FunnelPath
	}
	return svc
}

func serviceKeyForContainerService(cs *apptypes.ContainerService) string {
	serviceName := strings.TrimSpace(cs.ServiceName)
	if serviceName != "" {
		if port := strings.TrimSpace(cs.Port); port != "" {
			return serviceName + ":" + port
		}
		return serviceName
	}
	containerName := strings.TrimSpace(cs.ContainerName)
	if cs.FunnelEnabled {
		if port := strings.TrimSpace(firstNonEmpty(cs.FunnelFunnelPort, cs.FunnelPort)); port != "" {
			return containerName + ":funnel:" + port
		}
	}
	return containerName
}

// ---- event mapping -------------------------------------------------------

func (c *Collector) mapEvents(ctx context.Context, msg events.Message) []proto.Event {
	attrs := msg.Actor.Attributes
	if attrs == nil {
		attrs = map[string]string{}
	}
	bases := c.eventBases(msg, attrs)
	if len(bases) == 0 {
		return nil
	}
	action := string(msg.Action)

	switch {
	case msg.Action == events.ActionDie:
		out := make([]proto.Event, 0, len(bases)*2)
		for _, base := range bases {
			ev := base
			ev.Kind = proto.EventDie
			if code, ok := atoiPtr(attrs["exitCode"]); ok {
				ev.ExitCode = code
			}
			out = append(out, ev)
		}
		if rc := c.docker.RestartCount(ctx, msg.Actor.ID); rc > restartLoopThreshold {
			for _, base := range bases {
				loop := base
				loop.Kind = proto.EventRestartLoop
				loop.RestartCount = rc
				out = append(out, loop)
			}
		}
		return out
	case msg.Action == events.ActionOOM:
		return eventsWithKind(bases, proto.EventOOM)
	case msg.Action == events.ActionStart:
		return eventsWithKind(bases, proto.EventStart)
	case msg.Action == events.ActionStop || msg.Action == events.ActionRestart:
		out := eventsWithKind(bases, proto.EventStop)
		if msg.Action == events.ActionRestart {
			for i := range out {
				out[i].Message = "restart"
			}
		}
		return out
	case strings.HasPrefix(action, "health_status"):
		out := eventsWithKind(bases, proto.EventHealthStatus)
		for i := range out {
			out[i].HealthStatus = healthStatusFromEvent(action, attrs)
		}
		return out
	}
	return nil
}

func eventsWithKind(bases []proto.Event, kind proto.EventKind) []proto.Event {
	out := make([]proto.Event, 0, len(bases))
	for _, base := range bases {
		ev := base
		ev.Kind = kind
		out = append(out, ev)
	}
	return out
}

func (c *Collector) eventBases(msg events.Message, attrs map[string]string) []proto.Event {
	keys := c.serviceKeysForEvent(msg, attrs)
	if len(keys) == 0 {
		return nil
	}
	out := make([]proto.Event, 0, len(keys))
	for _, key := range keys {
		out = append(out, proto.Event{
			ContainerID:   msg.Actor.ID,
			ContainerName: attrs["name"],
			ServiceKey:    key,
			OccurredAt:    eventMillis(msg),
		})
	}
	return out
}

func (c *Collector) serviceKeysForEvent(msg events.Message, attrs map[string]string) []string {
	c.mu.RLock()
	latest := c.latest
	c.mu.RUnlock()

	var keys []string
	seen := map[string]struct{}{}
	for _, svc := range latest {
		if !sameContainer(msg.Actor.ID, attrs["name"], svc.ContainerID, svc.ContainerName) {
			continue
		}
		if strings.TrimSpace(svc.Key) == "" {
			continue
		}
		if _, ok := seen[svc.Key]; ok {
			continue
		}
		seen[svc.Key] = struct{}{}
		keys = append(keys, svc.Key)
	}
	if len(keys) > 0 {
		return keys
	}

	if key := serviceKeyFromAttrs(attrs); key != "" {
		return []string{key}
	}
	return nil
}

func sameContainer(eventID, eventName, serviceID, serviceName string) bool {
	eventID = strings.TrimSpace(eventID)
	serviceID = strings.TrimSpace(serviceID)
	if eventID != "" && serviceID != "" && (strings.HasPrefix(eventID, serviceID) || strings.HasPrefix(serviceID, eventID)) {
		return true
	}
	eventName = strings.TrimPrefix(strings.TrimSpace(eventName), "/")
	serviceName = strings.TrimPrefix(strings.TrimSpace(serviceName), "/")
	return eventName != "" && serviceName != "" && eventName == serviceName
}

func serviceKeyFromAttrs(attrs map[string]string) string {
	if sn := strings.TrimSpace(attrs[apptypes.LabelService]); sn != "" {
		if port := servicePortFromAttrs(attrs); port != "" {
			return sn + ":" + port
		}
		return sn
	}
	return attrs["name"]
}

func servicePortFromAttrs(attrs map[string]string) string {
	if port := strings.TrimSpace(attrs[apptypes.LabelPort]); port != "" {
		return port
	}
	if strings.TrimSpace(attrs[apptypes.LabelTarget]) == "" {
		return ""
	}
	if strings.TrimSpace(attrs[apptypes.LabelServiceProtocol]) == "https" {
		return "443"
	}
	return "80"
}

func healthStatusFromEvent(action string, attrs map[string]string) string {
	if hs := strings.TrimSpace(attrs["health_status"]); hs != "" {
		return hs
	}
	if idx := strings.Index(action, ":"); idx >= 0 {
		return strings.TrimSpace(action[idx+1:])
	}
	return ""
}

func eventMillis(msg events.Message) int64 {
	if msg.TimeNano > 0 {
		return msg.TimeNano / 1_000_000
	}
	return msg.Time * 1000
}

func atoiPtr(s string) (*int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil, false
	}
	return &n, true
}

// maybeCaptureLogs captures + sends a log excerpt for down-signal events unless
// the service's effective capture mode is off. Capture is on by default.
func (c *Collector) maybeCaptureLogs(ctx context.Context, conn *wsConn, ev proto.Event) {
	switch ev.Kind {
	case proto.EventDie, proto.EventOOM, proto.EventRestartLoop:
	default:
		return
	}
	if ev.ContainerID == "" || c.logModeFor(ev.ServiceKey) == proto.LogModeOff {
		return
	}
	excerpt, err := c.captureLogs(ctx, ev.ServiceKey, ev.ContainerID)
	if err != nil || excerpt == nil {
		return
	}
	c.send(conn, proto.TypeLogExcerpt, excerpt)
	c.log.Debug().Str("service", ev.ServiceKey).Int("lines", len(excerpt.Lines)).Msg("cloud: log excerpt sent")
}

// ---- connection lifecycle ------------------------------------------------

// Run is the reconnect loop. It blocks until ctx is cancelled. Each iteration
// dials, performs the hello handshake, and (on accept) serves until the
// connection drops, then backs off — unless the rejection is terminal.
func (c *Collector) Run(ctx context.Context) {
	bo := newBackoff()
	for ctx.Err() == nil {
		if c.session(ctx, bo) {
			return // terminal rejection
		}
		if ctx.Err() != nil {
			return
		}
		d := bo.next()
		c.log.Info().Dur("backoff", d).Msg("cloud: reconnecting after backoff")
		select {
		case <-ctx.Done():
			return
		case <-time.After(d):
		}
	}
}

// session runs one connection lifetime; stop=true means give up entirely.
func (c *Collector) session(ctx context.Context, bo *backoff) (stop bool) {
	dialCtx, dialCancel := context.WithTimeout(ctx, 20*time.Second)
	conn, err := dial(dialCtx, c.cfg.URL, c.cfg.Key, c.log)
	dialCancel()
	if err != nil {
		var de *dialError
		if asDialError(err, &de) && (de.statusCode == 401 || de.statusCode == 403) {
			c.log.Error().Int("status", de.statusCode).Msg("cloud: connection rejected (auth) — stopping")
			return true
		}
		c.log.Warn().Err(err).Msg("cloud: dial failed")
		return false
	}

	ackCh := make(chan proto.HelloAck, 1)
	h := handlers{
		onHelloAck: func(ack proto.HelloAck) {
			select {
			case ackCh <- ack:
			default:
			}
		},
		onConfig: func(cfg proto.Config) { c.applyConfig(cfg) },
	}

	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()

	runDone := make(chan error, 1)
	go func() { runDone <- conn.run(connCtx, h) }()

	if !c.sendHello(conn) {
		connCancel()
		<-runDone
		return false
	}

	select {
	case <-connCtx.Done():
		<-runDone
		return false
	case err := <-runDone:
		if err != nil {
			c.log.Warn().Err(err).Msg("cloud: connection closed before hello_ack")
		}
		return false
	case ack := <-ackCh:
		if !ack.Accepted {
			if terminalReject(ack.Reason) {
				c.log.Error().Str("reason", string(ack.Reason)).Msg("cloud: hello rejected (terminal) — stopping")
				connCancel()
				<-runDone
				return true
			}
			c.log.Warn().Str("reason", string(ack.Reason)).Msg("cloud: hello rejected — will retry")
			connCancel()
			<-runDone
			return false
		}
		c.log.Info().Str("host_id", ack.HostID).Int("config_version", ack.ConfigVersion).Msg("cloud: connected and accepted")
	case <-time.After(15 * time.Second):
		c.log.Warn().Msg("cloud: timed out waiting for hello_ack")
		connCancel()
		<-runDone
		return false
	}

	// Accepted. Publish the connection, send an immediate snapshot from the last
	// reconcile, and start the heartbeat + check loops.
	bo.reset()
	c.setConn(conn)
	c.sendCurrentSnapshot(conn)
	go c.discoverLoop(connCtx, conn)
	go c.heartbeatLoop(connCtx, conn)
	go c.checkLoop(connCtx, conn)

	err = <-runDone
	c.clearConn(conn)
	if err != nil {
		c.log.Warn().Err(err).Msg("cloud: connection closed")
	}
	return false
}

func (c *Collector) heartbeatLoop(ctx context.Context, conn *wsConn) {
	ticker := time.NewTicker(proto.HeartbeatInterval * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.send(conn, proto.TypeHeartbeat, proto.Heartbeat{Uptime: conn.uptime()})
		}
	}
}

func (c *Collector) checkLoop(ctx context.Context, conn *wsConn) {
	ticker := time.NewTicker(c.cfg.CheckInterval)
	defer ticker.Stop()
	c.runChecks(ctx, conn)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.runChecks(ctx, conn)
		}
	}
}

func (c *Collector) runChecks(ctx context.Context, conn *wsConn) {
	c.mu.RLock()
	services := c.latest
	configs := c.checks
	unmonitored := c.unmonitored
	c.mu.RUnlock()
	// Unmonitored: the cloud drops check_results, so don't even run the local
	// probes.
	if unmonitored || len(services) == 0 {
		return
	}
	results := c.checker.run(ctx, services, configs)
	if len(results) == 0 {
		return
	}
	c.send(conn, proto.TypeCheckResults, proto.CheckResults{Results: results})
	c.log.Debug().Int("results", len(results)).Msg("cloud: check results sent")
}

func (c *Collector) sendCurrentSnapshot(conn *wsConn) {
	c.mu.RLock()
	services := c.latest
	c.mu.RUnlock()
	if services == nil {
		services = []proto.Service{}
	}
	// Partial: the cached snapshot may be stale/running-only. discoverLoop sends
	// an authoritative full snapshot immediately after connect, so this initial
	// push must not drive removals.
	c.send(conn, proto.TypeSnapshot, proto.Snapshot{Services: services, Full: false})
}

// discoverLoop lists enabled containers straight from Docker on a ticker and
// pushes a snapshot. This makes cloud reporting self-sufficient: it does not
// depend on the reconciler's OnReconcile callback, which only fires after a
// *successful* tailscale serve reconcile. A host with no tailnet (or a failing
// one) therefore still reports its services to the cloud. OnReconcile remains
// wired as an additional, event-driven refresh when the reconcile does succeed.
func (c *Collector) discoverLoop(ctx context.Context, conn *wsConn) {
	ticker := time.NewTicker(c.cfg.CheckInterval)
	defer ticker.Stop()
	c.scanAndSnapshot(ctx, conn)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.scanAndSnapshot(ctx, conn)
		}
	}
}

// snapshotDue reports whether this discover tick should scan and send. A
// monitored host always sends; an unmonitored host sends only every
// unmonitoredSnapshotInterval (a low-rate catalog teaser), recording the send
// time so subsequent ticks back off.
func (c *Collector) snapshotDue() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.unmonitored {
		return true
	}
	if time.Since(c.lastTeaser) < unmonitoredSnapshotInterval {
		return false
	}
	c.lastTeaser = time.Now()
	return true
}

// scanAndSnapshot discovers enabled containers via Docker, caches the built
// services as the latest snapshot, and sends it.
func (c *Collector) scanAndSnapshot(ctx context.Context, conn *wsConn) {
	// Throttle the whole scan when unmonitored — skip the docker inspect/stats
	// work too, not just the send.
	if !c.snapshotDue() {
		return
	}
	containers, err := c.docker.GetCloudContainers(ctx)
	if err != nil {
		c.log.Warn().Err(err).Msg("cloud: container discovery failed")
		return
	}
	built := c.buildServices(ctx, containers)
	c.mu.Lock()
	c.latest = built
	c.mu.Unlock()
	// Full: self-discovery includes stopped containers, so it is authoritative
	// for presence — the cloud uses it to detect removed services.
	c.send(conn, proto.TypeSnapshot, proto.Snapshot{Services: built, Full: true})
	c.log.Debug().Int("services", len(built)).Msg("cloud: snapshot sent (self-discovered)")
}

// ---- helpers -------------------------------------------------------------

func (c *Collector) sendHello(conn *wsConn) bool {
	hello := proto.Hello{
		ProtocolVersion: proto.ProtocolVersion,
		Fingerprint:     c.fingerprint,
		Hostname:        c.hostname,
		AgentVersion:    agentVersion,
		DockerVersion:   c.dockerVersion,
		Capabilities:    []string{"http_checks", "log_capture", "container_stats"},
		OS:              c.specs.OS,
		KernelVersion:   c.specs.KernelVersion,
		Arch:            c.specs.Arch,
		CPUCores:        c.specs.CPUCores,
		MemTotalBytes:   c.specs.MemTotalBytes,
	}
	env, err := proto.Encode(proto.TypeHello, nowMillis(), hello)
	if err != nil {
		c.log.Error().Err(err).Msg("cloud: encode hello failed")
		return false
	}
	return conn.sendFrame(env)
}

func (c *Collector) send(conn *wsConn, t proto.MessageType, msg any) {
	env, err := proto.Encode(t, nowMillis(), msg)
	if err != nil {
		c.log.Warn().Err(err).Str("type", string(t)).Msg("cloud: encode failed")
		return
	}
	conn.sendFrame(env)
}

func (c *Collector) setConn(conn *wsConn) {
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
}

func (c *Collector) clearConn(conn *wsConn) {
	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	c.mu.Unlock()
}

func (c *Collector) applyConfig(cfg proto.Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cfg.Version < c.cfgVer {
		return
	}
	c.cfgVer = cfg.Version
	c.checks = cfg.Checks

	// On the monitored→unmonitored transition, zero the teaser timer so the next
	// discover tick emits one snapshot immediately (populating the catalog teaser)
	// before settling into the throttled cadence.
	if cfg.Unmonitored && !c.unmonitored {
		c.lastTeaser = time.Time{}
	}
	c.unmonitored = cfg.Unmonitored

	mode := cfg.Logs.Mode
	overrides := cfg.Logs.Overrides
	if mode == "" && len(overrides) == 0 && len(cfg.LogOptIn) > 0 {
		// Legacy config from an older cloud: only the listed service keys
		// captured (on incident). Map that onto the mode model so behaviour is
		// unchanged against an older control plane.
		mode = proto.LogModeOff
		overrides = make(map[string]string, len(cfg.LogOptIn))
		for _, k := range cfg.LogOptIn {
			overrides[k] = proto.LogModeIncident
		}
	}
	c.logMode = mode
	c.logOverrides = overrides

	logMode := mode
	if logMode == "" {
		logMode = proto.LogModeIncident
	}
	c.log.Info().Int("version", cfg.Version).Int("checks", len(cfg.Checks)).Str("log_mode", logMode).Int("log_overrides", len(overrides)).Bool("unmonitored", cfg.Unmonitored).Msg("cloud: applied config")
}

// logModeFor returns the effective capture mode for a service key: its override
// when set, else the workspace default, else the built-in default (incident).
func (c *Collector) logModeFor(serviceKey string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if m := c.logOverrides[serviceKey]; m != "" {
		return m
	}
	if c.logMode == "" {
		return proto.LogModeIncident
	}
	return c.logMode
}

func terminalReject(reason proto.RejectCode) bool {
	switch reason {
	case proto.RejectInvalidKey, proto.RejectBlocked, proto.RejectProtocolMismatch:
		return true
	default:
		return false
	}
}

func asDialError(err error, target **dialError) bool {
	for err != nil {
		if de, ok := err.(*dialError); ok {
			*target = de
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
