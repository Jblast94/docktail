package cloud

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/marvinvr/docktail/cloud/proto"
)

// captureLogs reads the last lines of a container's logs and returns a capped
// excerpt. Callers gate this on the effective cloud-pushed LogConfig. There is
// no container label for this.
func (c *Collector) captureLogs(ctx context.Context, serviceKey, containerID string) (*proto.LogExcerpt, error) {
	lines, _, err := c.docker.ContainerLogsTail(ctx, containerID, proto.MaxLogLines)
	if err != nil {
		return nil, err
	}
	lines = redactLogLines(lines)
	lines, size := capLines(lines)
	return &proto.LogExcerpt{
		ServiceKey:  serviceKey,
		ContainerID: containerID,
		Lines:       lines,
		ByteSize:    size,
		CapturedAt:  nowMillis(),
	}, nil
}

const redactedValue = "[REDACTED]"

var (
	authorizationPattern = regexp.MustCompile(`(?i)(\b(?:proxy[_-]?)?authorization\s*[:=]\s*)[^\r\n,;]+`)
	cookieHeaderPattern  = regexp.MustCompile(`(?i)(\b(?:cookie|set-cookie)\s*[:=]\s*).*$`)
	bearerPattern        = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]+`)
	secretValuePattern   = regexp.MustCompile(`(?i)(["']?(?:authorization|proxy[_-]?authorization|x[_-]?api[_-]?key|api[_-]?key|aws[_-]?access[_-]?key[_-]?id|aws[_-]?secret[_-]?access[_-]?key|access[_-]?token|auth[_-]?token|refresh[_-]?token|session[_-]?(?:id|token)|password|passwd|client[_-]?secret|private[_-]?(?:key|token)|docktail[_-]?cloud[_-]?key|secret|token|cookie|set-cookie)["']?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	credentialURLPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^:/\s]+:)[^@\s]+@`)
	knownTokenPattern    = regexp.MustCompile(`(?i)\b(?:dtc_|gh[pousr]_|github_pat_|xox[baprs]-)[a-z0-9_-]{8,}`)
	awsAccessKeyPattern  = regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)
	jwtPattern           = regexp.MustCompile(`\beyJ[a-zA-Z0-9_-]{5,}\.[a-zA-Z0-9_-]{5,}\.[a-zA-Z0-9_-]{5,}\b`)
)

// redactLogLines removes common credential shapes before any excerpt leaves
// the host. It is deliberately conservative and best-effort; it complements,
// but cannot replace, keeping secrets out of application logs.
func redactLogLines(lines []string) []string {
	redacted := make([]string, len(lines))
	inPrivateKey := false
	for i, line := range lines {
		upper := strings.ToUpper(strings.TrimSpace(line))
		if strings.Contains(upper, "-----BEGIN ") && strings.Contains(upper, "PRIVATE KEY-----") {
			inPrivateKey = !strings.Contains(upper, "-----END ")
			redacted[i] = redactedValue + " PRIVATE KEY"
			continue
		}
		if inPrivateKey {
			redacted[i] = redactedValue
			if strings.Contains(upper, "-----END ") && strings.Contains(upper, "PRIVATE KEY-----") {
				inPrivateKey = false
			}
			continue
		}

		line = authorizationPattern.ReplaceAllString(line, `${1}`+redactedValue)
		line = cookieHeaderPattern.ReplaceAllString(line, `${1}`+redactedValue)
		line = bearerPattern.ReplaceAllString(line, "Bearer "+redactedValue)
		line = secretValuePattern.ReplaceAllString(line, `${1}`+redactedValue)
		line = credentialURLPattern.ReplaceAllString(line, `${1}`+redactedValue+"@")
		line = knownTokenPattern.ReplaceAllString(line, redactedValue)
		line = awsAccessKeyPattern.ReplaceAllString(line, redactedValue)
		line = jwtPattern.ReplaceAllString(line, redactedValue)
		redacted[i] = line
	}
	return redacted
}

// capLines enforces the line and byte caps, keeping the most recent lines.
func capLines(lines []string) ([]string, int) {
	if len(lines) > proto.MaxLogLines {
		lines = lines[len(lines)-proto.MaxLogLines:]
	}
	total := 0
	for _, l := range lines {
		total += len(l) + 1
	}
	for total > proto.MaxLogBytes && len(lines) > 1 {
		total -= len(lines[0]) + 1
		lines = lines[1:]
	}
	if total > proto.MaxLogBytes && len(lines) == 1 {
		maxLineBytes := proto.MaxLogBytes - 1
		if maxLineBytes < 0 {
			maxLineBytes = 0
		}
		if len(lines[0]) > maxLineBytes {
			for maxLineBytes > 0 && !utf8.ValidString(lines[0][:maxLineBytes]) {
				maxLineBytes--
			}
			lines[0] = lines[0][:maxLineBytes]
		}
		total = len(lines[0]) + 1
	}
	if total < 0 {
		total = 0
	}
	return lines, total
}
