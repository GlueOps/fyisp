//go:build linux

package hops

import (
	"context"
	"net/netip"
	"time"
)

// ProbeTransport on Linux uses the socket's own error queue (errqTransport),
// whatever ICMP socket the prober reads.
func (t *packetProber) ProbeTransport(ctx context.Context, tp Transport, dst netip.AddrPort, ttl int, timeout time.Duration) (Hop, error) {
	return errqTransport(ctx, tp, dst, 0, ttl, timeout)
}

func (t *packetProber) ProbeFlow(ctx context.Context, dst netip.AddrPort, sport uint16, ttl int, timeout time.Duration) (Hop, error) {
	return errqTransport(ctx, TCP, dst, sport, ttl, timeout)
}
