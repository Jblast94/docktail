package cloud

import (
	"context"

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
	lines, size := capLines(lines)
	return &proto.LogExcerpt{
		ServiceKey:  serviceKey,
		ContainerID: containerID,
		Lines:       lines,
		ByteSize:    size,
		CapturedAt:  nowMillis(),
	}, nil
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
	for total > proto.MaxLogBytes && len(lines) > 0 {
		total -= len(lines[0]) + 1
		lines = lines[1:]
	}
	if total > proto.MaxLogBytes && len(lines) == 1 {
		over := total - proto.MaxLogBytes
		if l := lines[0]; over < len(l) {
			lines[0] = l[:len(l)-over]
			total = proto.MaxLogBytes
		}
	}
	if total < 0 {
		total = 0
	}
	return lines, total
}
