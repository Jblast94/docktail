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
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
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
	// It is a documented local-development escape hatch, not production
	// customer configuration.
	EnvURL = "DOCKTAIL_CLOUD_URL"
	// EnvAllowInsecure permits an explicitly configured ws:// endpoint whose
	// host is not loopback. It is a development-only escape hatch.
	EnvAllowInsecure = "DOCKTAIL_CLOUD_ALLOW_INSECURE"
	EnvLogLevel      = "DOCKTAIL_LOG_LEVEL"
	EnvCheckPeriod   = "DOCKTAIL_CHECK_INTERVAL"
)

// Defaults.
const (
	DefaultCheckInterval = 30 * time.Second
	DefaultLogLevel      = "info"
	MinCheckInterval     = time.Duration(proto.MinCheckIntervalMS) * time.Millisecond
	MaxCheckInterval     = time.Duration(proto.MaxCheckIntervalMS) * time.Millisecond
)

// Config is the resolved cloud-module configuration.
type Config struct {
	Key           string        // workspace key (dtc_...). Empty means disabled.
	URL           string        // WSS ingest endpoint.
	AllowInsecure bool          // development-only non-loopback ws:// escape hatch.
	LogLevel      string        // zerolog level name (informational; main owns logging).
	CheckInterval time.Duration // how often local-vantage checks run.
}

// LoadConfig reads the cloud configuration from the environment, applying
// defaults.
func LoadConfig() Config {
	return Config{
		Key:           strings.TrimSpace(os.Getenv(EnvKey)),
		URL:           getEnv(EnvURL, proto.DefaultEndpoint),
		AllowInsecure: getEnvBool(EnvAllowInsecure),
		LogLevel:      getEnv(EnvLogLevel, DefaultLogLevel),
		CheckInterval: getEnvDuration(EnvCheckPeriod, DefaultCheckInterval),
	}
}

// validate fills programmatic zero values with production defaults and rejects
// endpoints that could send a workspace key over plaintext unexpectedly.
func (c *Config) validate() error {
	c.Key = strings.TrimSpace(c.Key)
	keyParts := strings.Fields(c.Key)
	if len(keyParts) != 1 || keyParts[0] != c.Key ||
		!strings.HasPrefix(c.Key, proto.KeyPrefix) ||
		len(c.Key) < len(proto.KeyPrefix)+16 || len(c.Key) > 256 {
		return fmt.Errorf("%s is not a valid DockTail Cloud workspace key", EnvKey)
	}
	if strings.TrimSpace(c.URL) == "" {
		c.URL = proto.DefaultEndpoint
	}
	if c.CheckInterval == 0 {
		c.CheckInterval = DefaultCheckInterval
	}
	if c.CheckInterval < MinCheckInterval || c.CheckInterval > MaxCheckInterval {
		return fmt.Errorf("%s must be between %s and %s", EnvCheckPeriod, MinCheckInterval, MaxCheckInterval)
	}

	endpoint, err := url.Parse(c.URL)
	if err != nil || endpoint.Host == "" {
		return fmt.Errorf("%s must be an absolute ws:// or wss:// URL", EnvURL)
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return fmt.Errorf("%s must not contain user info, a query, or a fragment", EnvURL)
	}
	switch endpoint.Scheme {
	case "wss":
		return nil
	case "ws":
		if loopbackHost(endpoint.Hostname()) || c.AllowInsecure {
			return nil
		}
		return fmt.Errorf("%s requires wss:// for non-loopback endpoints (set %s=true only for local development)", EnvURL, EnvAllowInsecure)
	default:
		return fmt.Errorf("%s must use ws:// or wss://", EnvURL)
	}
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		return -1
	}
	return def
}

func getEnvBool(key string) bool {
	value := strings.TrimSpace(os.Getenv(key))
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}
