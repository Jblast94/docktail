package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/rs/zerolog/log"

	apptypes "github.com/marvinvr/docktail/types"
)

// This file adds read-only helpers used by the optional cloud module
// (see ../cloud). They reuse the same docker client connection rather than
// opening a second one, and never affect reconciliation behavior. None of these
// are used unless DOCKTAIL_CLOUD_KEY is set.

// CloudInfo is the runtime enrichment the cloud catalog needs that the
// reconciler's ContainerService does not already carry.
type CloudInfo struct {
	Image          string
	ImageTag       string
	State          string // running/exited/restarting/paused/created
	Health         string // healthy/unhealthy/starting (empty if no healthcheck)
	RestartCount   int
	ComposeProject string
	ComposeService string
	Networks       []string
}

// GetCloudContainers lists docktail-managed containers for cloud reporting,
// INCLUDING stopped/exited ones — unlike GetEnabledContainers, which is
// running-only because the serve reconciler must never target a dead container.
// Reporting stopped containers lets the cloud render them as down rather than
// dropping them, and reserves "removed" for containers that truly leave Docker.
//
// Running containers are parsed at full fidelity (multiple/indexed services and
// funnel included). A non-running container whose live parse fails — direct mode
// needs a running IP — falls back to a minimal entry keyed by the same primary
// service name it carries when running, so it stays in the catalog as a down
// service instead of being seen as removed. Limitation: a stopped container that
// declares multiple indexed services reports only its primary service until it
// is running again.
func (c *Client) GetCloudContainers(ctx context.Context) ([]*apptypes.ContainerService, error) {
	containers, err := c.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var services []*apptypes.ContainerService
	for _, cont := range containers {
		if !isManagedContainer(cont.Labels) {
			continue
		}

		parsed, perr := c.parseContainer(ctx, cont.ID, cont.Labels)
		if perr == nil {
			services = append(services, parsed...)
			continue
		}

		name := ""
		if len(cont.Names) > 0 {
			name = strings.TrimPrefix(cont.Names[0], "/")
		}
		// A *running* container that fails to parse is a genuine problem — skip it,
		// matching GetEnabledContainers. A non-running one is expected to fail
		// (e.g. direct mode has no live IP); keep a minimal down entry so the cloud
		// still sees the service.
		if cont.State == "running" {
			log.Warn().Err(perr).Str("container", name).Msg("cloud: failed to parse running container, skipping")
			continue
		}
		id := cont.ID
		if len(id) > 12 {
			id = id[:12]
		}
		services = append(services, &apptypes.ContainerService{
			ContainerID:    id,
			ContainerName:  name,
			ServiceEnabled: isServiceEnabled(cont.Labels),
			ServiceName:    cont.Labels[apptypes.LabelService],
		})
	}
	return services, nil
}

// EngineID returns the docker engine ID — the stable host fingerprint and the
// cloud's billable unit. Persisted by dockerd; survives reboots/reinstalls.
func (c *Client) EngineID(ctx context.Context) (string, error) {
	info, err := c.cli.Info(ctx)
	if err != nil {
		return "", fmt.Errorf("docker info: %w", err)
	}
	if info.ID == "" {
		return "", fmt.Errorf("docker engine ID is empty")
	}
	return info.ID, nil
}

// ServerVersion returns the docker server version string (best-effort).
func (c *Client) ServerVersion(ctx context.Context) string {
	v, err := c.cli.ServerVersion(ctx)
	if err != nil {
		return ""
	}
	return v.Version
}

// HostSpec is static host capacity read once from `docker info` (display-only).
type HostSpec struct {
	OS            string
	KernelVersion string
	Arch          string
	CPUCores      int
	MemTotalBytes int64
}

// HostSpecs reads static host capacity from `docker info` (best-effort; zero
// values on error). Read once at agent start — these do not change at runtime.
func (c *Client) HostSpecs(ctx context.Context) HostSpec {
	info, err := c.cli.Info(ctx)
	if err != nil {
		return HostSpec{}
	}
	return HostSpec{
		OS:            info.OperatingSystem,
		KernelVersion: info.KernelVersion,
		Arch:          info.Architecture,
		CPUCores:      info.NCPU,
		MemTotalBytes: info.MemTotal,
	}
}

// Hostname returns the host's name as docker reports it (display-only).
func (c *Client) Hostname(ctx context.Context) string {
	info, err := c.cli.Info(ctx)
	if err != nil {
		return ""
	}
	return info.Name
}

// RestartCount best-effort reads a container's live restart count via inspect.
func (c *Client) RestartCount(ctx context.Context, containerID string) int {
	if containerID == "" {
		return 0
	}
	in, err := c.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return 0
	}
	return in.RestartCount
}

// InspectCloud inspects a container and extracts the runtime fields the cloud
// catalog wants. It is read-only.
func (c *Client) InspectCloud(ctx context.Context, containerID string) (CloudInfo, error) {
	in, err := c.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return CloudInfo{}, fmt.Errorf("inspect container: %w", err)
	}

	var info CloudInfo
	image := in.Image
	var labels map[string]string
	if in.Config != nil {
		labels = in.Config.Labels
		if in.Config.Image != "" {
			image = in.Config.Image
		}
	}
	info.Image, info.ImageTag = splitImageTag(image)
	info.RestartCount = in.RestartCount

	if labels != nil {
		info.ComposeProject = labels["com.docker.compose.project"]
		info.ComposeService = labels["com.docker.compose.service"]
	}

	if in.State != nil {
		info.State = in.State.Status
		if in.State.Health != nil {
			info.Health = in.State.Health.Status
		}
	}

	if in.NetworkSettings != nil {
		for name := range in.NetworkSettings.Networks {
			info.Networks = append(info.Networks, name)
		}
	}

	return info, nil
}

// ContainerStatsSample is a single one-shot resource reading for a container.
// CPU is reported as the raw cumulative counters docker exposes; a percentage
// is the delta between two samples, so the caller keeps the previous one.
// Memory is the cache-adjusted working set and its effective limit, both ready
// to use directly.
type ContainerStatsSample struct {
	CPUTotalUsage  uint64 // cumulative container CPU time (ns)
	CPUSystemUsage uint64 // cumulative host CPU time (ns)
	OnlineCPUs     uint64 // CPUs available, for percentage normalization
	MemUsageBytes  int64  // working set: usage minus inactive file cache
	MemLimitBytes  int64  // effective limit: container limit, else host total
}

// ContainerStats reads a single (non-streaming) docker stats sample for a
// container via the one-shot stats endpoint. It is read-only and used only by
// the optional cloud module. PreCPUStats is intentionally ignored — it is zero
// for a one-shot read, so CPU percentages are computed by the caller across
// successive samples.
func (c *Client) ContainerStats(ctx context.Context, containerID string) (ContainerStatsSample, error) {
	resp, err := c.cli.ContainerStatsOneShot(ctx, containerID)
	if err != nil {
		return ContainerStatsSample{}, fmt.Errorf("container stats: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var v container.StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return ContainerStatsSample{}, fmt.Errorf("decode stats: %w", err)
	}

	onlineCPUs := uint64(v.CPUStats.OnlineCPUs)
	if onlineCPUs == 0 {
		onlineCPUs = uint64(len(v.CPUStats.CPUUsage.PercpuUsage))
	}
	return ContainerStatsSample{
		CPUTotalUsage:  v.CPUStats.CPUUsage.TotalUsage,
		CPUSystemUsage: v.CPUStats.SystemUsage,
		OnlineCPUs:     onlineCPUs,
		MemUsageBytes:  memUsageNoCache(v.MemoryStats),
		MemLimitBytes:  int64(v.MemoryStats.Limit),
	}, nil
}

// memUsageNoCache returns the container's working set — total memory usage minus
// the inactive file cache — matching what `docker stats` shows. The cache key
// differs between cgroup v1 (total_inactive_file) and v2 (inactive_file); falls
// back to the raw usage when neither is present.
func memUsageNoCache(mem container.MemoryStats) int64 {
	if v, ok := mem.Stats["total_inactive_file"]; ok && v < mem.Usage { // cgroup v1
		return int64(mem.Usage - v)
	}
	if v, ok := mem.Stats["inactive_file"]; ok && v < mem.Usage { // cgroup v2
		return int64(mem.Usage - v)
	}
	return int64(mem.Usage)
}

// ContainerLogsTail returns the last n log lines of a container plus the total
// captured byte size, reading both stdout and stderr.
func (c *Client) ContainerLogsTail(ctx context.Context, containerID string, n int) ([]string, int, error) {
	rc, err := c.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       strconv.Itoa(n),
		Timestamps: false,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("container logs: %w", err)
	}
	defer func() { _ = rc.Close() }()

	var lines []string
	total := 0
	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		s := string(stripLogHeader(scanner.Bytes()))
		lines = append(lines, s)
		total += len(s) + 1
	}
	if err := scanner.Err(); err != nil {
		return lines, total, fmt.Errorf("read logs: %w", err)
	}
	if n > 0 && len(lines) > n {
		drop := len(lines) - n
		for _, d := range lines[:drop] {
			total -= len(d) + 1
		}
		lines = lines[drop:]
	}
	if total < 0 {
		total = 0
	}
	return lines, total, nil
}

// splitImageTag splits "repo:tag" into ("repo", "tag"), registry-aware: a colon
// in a registry host:port (which has a "/" after it) is not a tag.
func splitImageTag(image string) (repo, tag string) {
	if image == "" {
		return "", ""
	}
	idx := strings.LastIndex(image, ":")
	if idx < 0 {
		return image, ""
	}
	if strings.Contains(image[idx:], "/") {
		return image, ""
	}
	return image[:idx], image[idx+1:]
}

// stripLogHeader removes the 8-byte multiplexing header docker prepends to each
// log frame when the container has no TTY. Returned unchanged for TTY containers.
func stripLogHeader(b []byte) []byte {
	if len(b) >= 8 && b[0] <= 2 && b[1] == 0 && b[2] == 0 && b[3] == 0 {
		return bytes.TrimRight(b[8:], "\r")
	}
	return bytes.TrimRight(b, "\r")
}
