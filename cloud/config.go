// Package cloud is DockTail's optional reporting module for DockTail Cloud. It
// is completely inert unless DOCKTAIL_CLOUD_KEY is set: with no key, none of it
// runs and no connection is opened. When enabled, it subscribes to the
// reconciler's results, periodically self-discovers Docker services for an
// authoritative cloud snapshot, watches docker failure signals, runs
// local-vantage checks, and streams metadata to the cloud over an outbound WSS
// link.
//
// The protocol is metadata-only by design (see ./proto): there are no
// exec/deploy/shell message types and this module never executes anything on the
// host. It depends only on the reconciler, the docker/tailscale clients, and the
// proto package — never on the proprietary cloud.
package cloud

import (
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/marvinvr/docktail/cloud/proto"
)

// Environment variables. The operator's required config is exactly one var
// (DOCKTAIL_CLOUD_KEY); everything else has a sane default.
const (
	EnvKey = "DOCKTAIL_CLOUD_KEY"
	// EnvURL overrides the baked-in ingest endpoint (proto.DefaultEndpoint).
	// Intentionally undocumented and unadvertised: the cloud URL is hard-coded
	// and customers never set this. It exists solely as a local-development
	// escape hatch, e.g. ws://localhost:8080/v1/agent.
	EnvURL         = "DOCKTAIL_CLOUD_URL"
	EnvLogLevel    = "DOCKTAIL_LOG_LEVEL"
	EnvCheckPeriod = "DOCKTAIL_CHECK_INTERVAL"
	// EnvHostProc / EnvHostSys redirect where whole-host vitals are read from
	// (default /proc and /sys). The agent normally reads the host's own,
	// non-namespaced /proc with no extra mounts. The exception is a Proxmox LXC:
	// the container's /proc bypasses the LXC's LXCFS layer and reports the
	// physical node instead of the container, so memory/CPU/load are wrong. On
	// such hosts, bind-mount the LXCFS files (/proc/{meminfo,stat,loadavg}) to an
	// alternate path and point these here. See docs/agent.md "Host vitals".
	EnvHostProc = "DOCKTAIL_HOST_PROC"
	EnvHostSys  = "DOCKTAIL_HOST_SYS"
)

// Defaults.
const (
	DefaultCheckInterval = 30 * time.Second
	DefaultLogLevel      = "info"
	DefaultHostProc      = "/proc"
	DefaultHostSys       = "/sys"
)

// Config is the resolved cloud-module configuration.
type Config struct {
	Key           string        // workspace key (dtc_...). Empty means disabled.
	URL           string        // WSS ingest endpoint.
	LogLevel      string        // zerolog level name (informational; main owns logging).
	CheckInterval time.Duration // how often local-vantage checks run.
	HostProc      string        // path to read whole-host /proc vitals from (default /proc).
	HostSys       string        // path to read whole-host /sys vitals from (default /sys).
}

// LoadConfig reads the cloud configuration from the environment, applying
// defaults.
func LoadConfig() Config {
	return Config{
		Key:           strings.TrimSpace(os.Getenv(EnvKey)),
		URL:           getEnv(EnvURL, proto.DefaultEndpoint),
		LogLevel:      getEnv(EnvLogLevel, DefaultLogLevel),
		CheckInterval: getEnvDuration(EnvCheckPeriod, DefaultCheckInterval),
		HostProc:      getEnv(EnvHostProc, DefaultHostProc),
		HostSys:       getEnv(EnvHostSys, DefaultHostSys),
	}
}

// Enabled reports whether the cloud module is active. DockTail runs exactly as
// before unless a workspace key is present.
func Enabled() bool {
	return strings.TrimSpace(os.Getenv(EnvKey)) != ""
}

// ZerologLevel maps the configured level name onto a zerolog.Level.
func (c Config) ZerologLevel() zerolog.Level {
	switch strings.ToLower(strings.TrimSpace(c.LogLevel)) {
	case "trace":
		return zerolog.TraceLevel
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

func getEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}
