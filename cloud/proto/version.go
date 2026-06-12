package proto

// ProtocolVersion is the wire protocol version. Bumped on breaking changes to
// the envelope or message payloads. Reported by the agent in [Hello] and
// echoed by the cloud in [HelloAck]; a mismatch the cloud cannot serve is a
// hard reject.
const ProtocolVersion = 1

// DefaultEndpoint is the cloud ingest URL, hard-coded into the agent. There is
// no customer-facing URL setting — config is exactly one env var,
// DOCKTAIL_CLOUD_KEY. (A dev-only override lives in cloud/config.go.)
const DefaultEndpoint = "wss://ingest.docktail.org/v1/agent"

// IngestPath is the WSS route the agent connects to on the agent-plane binary.
const IngestPath = "/v1/agent"

// KeyPrefix is the human-visible prefix of a workspace key. The full key is
// KeyPrefix followed by a random 256-bit value, base32/hex encoded. Only its
// SHA-256 hash is ever stored server-side.
const KeyPrefix = "dtc_"

// HeartbeatInterval (seconds) is how often the agent emits a [Heartbeat]. The
// cloud treats a host as offline after roughly 3 missed intervals.
const HeartbeatInterval = 30
