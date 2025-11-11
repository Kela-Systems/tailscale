// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// Package netdisrupt implements network disruption for testing purposes.
// It provides wrappers around net connections that can simulate various
// network conditions such as packet loss, bandwidth throttling, latency,
// and errors.
package netdisrupt

import (
	"errors"
	"math/rand/v2"
	"net"
	"net/netip"
	"sync"
	"time"

	"tailscale.com/types/nettype"
)

// DisruptionRate represents a rate between 0 and 1 for disrupting operations.
type DisruptionRate struct {
	rate float64
}

// NewDisruptionRate creates a new DisruptionRate ensuring the value is between 0 and 1.
func NewDisruptionRate(rate float64) (DisruptionRate, error) {
	if rate < 0 || rate > 1 {
		return DisruptionRate{}, errors.New("disruption rate must be between 0 and 1")
	}
	return DisruptionRate{rate: rate}, nil
}

// MustDisruptionRate creates a new DisruptionRate and panics if the rate is invalid.
func MustDisruptionRate(rate float64) DisruptionRate {
	d, err := NewDisruptionRate(rate)
	if err != nil {
		panic(err)
	}
	return d
}

// ShouldDisrupt returns true if an operation should be disrupted based on this rate.
func (d DisruptionRate) ShouldDisrupt() bool {
	return rand.Float64() < d.rate
}

// DisruptionOptions configures various network disruption parameters.
type DisruptionOptions struct {
	// ReadDropRate is the probability (0-1) that a read operation will be dropped
	ReadDropRate DisruptionRate
	// WriteDropRate is the probability (0-1) that a write operation will be dropped
	WriteDropRate DisruptionRate
	// ReadErrorRate is the probability (0-1) that a read operation will return an error
	ReadErrorRate DisruptionRate
	// WriteErrorRate is the probability (0-1) that a write operation will return an error
	WriteErrorRate DisruptionRate
	// MaxReadBandwidth limits read bandwidth in bytes per second, 0 means unlimited
	MaxReadBandwidth int
	// MaxWriteBandwidth limits write bandwidth in bytes per second, 0 means unlimited
	MaxWriteBandwidth int
	// ReadDelayMs adds additional delay to read operations in milliseconds
	ReadDelayMs int
	// WriteDelayMs adds additional delay to write operations in milliseconds
	WriteDelayMs int
}

// Predefined network disruption profiles for testing various mobile network conditions.
var (
	// DisruptionProfileNone represents no network disruption (normal operation).
	DisruptionProfileNone = &DisruptionOptions{}

	// DisruptionProfile2GEdge simulates poor 2G EDGE connection (100-150 Kbps, high latency, packet loss).
	DisruptionProfile2GEdge = &DisruptionOptions{
		ReadDropRate:      MustDisruptionRate(0.08), // 8% packet drop
		WriteDropRate:     MustDisruptionRate(0.08), // 8% packet drop
		ReadErrorRate:     MustDisruptionRate(0.02), // 2% error rate
		WriteErrorRate:    MustDisruptionRate(0.02), // 2% error rate
		MaxReadBandwidth:  15 * 1024,                // 15 KB/s (~120 Kbps)
		MaxWriteBandwidth: 10 * 1024,                // 10 KB/s (~80 Kbps)
		ReadDelayMs:       300,                      // 300ms read delay
		WriteDelayMs:      350,                      // 350ms write delay
	}

	// DisruptionProfile3G simulates typical 3G connection (384 Kbps, moderate latency, some packet loss).
	DisruptionProfile3G = &DisruptionOptions{
		ReadDropRate:      MustDisruptionRate(0.03), // 3% packet drop
		WriteDropRate:     MustDisruptionRate(0.03), // 3% packet drop
		ReadErrorRate:     MustDisruptionRate(0.01), // 1% error rate
		WriteErrorRate:    MustDisruptionRate(0.01), // 1% error rate
		MaxReadBandwidth:  48 * 1024,                // 48 KB/s (~384 Kbps)
		MaxWriteBandwidth: 32 * 1024,                // 32 KB/s (~256 Kbps)
		ReadDelayMs:       150,                      // 150ms read delay
		WriteDelayMs:      180,                      // 180ms write delay
	}

	// DisruptionProfileLTE simulates degraded LTE connection (5 Mbps, low latency, minimal packet loss).
	DisruptionProfileLTE = &DisruptionOptions{
		ReadDropRate:      MustDisruptionRate(0.01),  // 1% packet drop
		WriteDropRate:     MustDisruptionRate(0.01),  // 1% packet drop
		ReadErrorRate:     MustDisruptionRate(0.005), // 0.5% error rate
		WriteErrorRate:    MustDisruptionRate(0.005), // 0.5% error rate
		MaxReadBandwidth:  625 * 1024,                // 625 KB/s (~5 Mbps)
		MaxWriteBandwidth: 512 * 1024,                // 512 KB/s (~4 Mbps)
		ReadDelayMs:       50,                        // 50ms read delay
		WriteDelayMs:      60,                        // 60ms write delay
	}

	// DisruptionProfileHighLatency simulates connection with high latency but decent bandwidth.
	DisruptionProfileHighLatency = &DisruptionOptions{
		ReadDropRate:      MustDisruptionRate(0.02), // 2% packet drop
		WriteDropRate:     MustDisruptionRate(0.02), // 2% packet drop
		ReadErrorRate:     MustDisruptionRate(0.01), // 1% error rate
		WriteErrorRate:    MustDisruptionRate(0.01), // 1% error rate
		MaxReadBandwidth:  256 * 1024,               // 256 KB/s (~2 Mbps)
		MaxWriteBandwidth: 256 * 1024,               // 256 KB/s (~2 Mbps)
		ReadDelayMs:       500,                      // 500ms delay (satellite-like latency)
		WriteDelayMs:      500,                      // 500ms delay
	}

	// DisruptionProfileUnstable simulates very unstable connection with high packet loss.
	DisruptionProfileUnstable = &DisruptionOptions{
		ReadDropRate:      MustDisruptionRate(0.15), // 15% packet drop
		WriteDropRate:     MustDisruptionRate(0.15), // 15% packet drop
		ReadErrorRate:     MustDisruptionRate(0.05), // 5% error rate
		WriteErrorRate:    MustDisruptionRate(0.05), // 5% error rate
		MaxReadBandwidth:  64 * 1024,                // 64 KB/s (~512 Kbps)
		MaxWriteBandwidth: 64 * 1024,                // 64 KB/s (~512 Kbps)
		ReadDelayMs:       200,                      // 200ms delay
		WriteDelayMs:      200,                      // 200ms delay
	}
)

var (
	// ErrPacketDropped is returned when a packet is intentionally dropped due to disruption.
	ErrPacketDropped = errors.New("packet dropped by network disruption")
	// ErrDisruptionError is returned when a disruption error is triggered.
	ErrDisruptionError = errors.New("network disruption error")
)

// PacketConn wraps a nettype.PacketConn with network disruption capabilities.
type PacketConn struct {
	underlying nettype.PacketConn
	opts       *DisruptionOptions

	// Bandwidth throttling state
	mu              sync.Mutex
	readTokens      float64
	writeTokens     float64
	lastReadRefill  time.Time
	lastWriteRefill time.Time
}

// WrapPacketConn wraps a nettype.PacketConn with the specified disruption options.
func WrapPacketConn(conn nettype.PacketConn, opts *DisruptionOptions) *PacketConn {
	if opts == nil {
		opts = DisruptionProfileNone
	}
	now := time.Now()
	return &PacketConn{
		underlying:      conn,
		opts:            opts,
		readTokens:      float64(opts.MaxReadBandwidth),
		writeTokens:     float64(opts.MaxWriteBandwidth),
		lastReadRefill:  now,
		lastWriteRefill: now,
	}
}

// applyDelay adds configured delay to an operation.
func (c *PacketConn) applyDelay(delayMs int) {
	if delayMs > 0 {
		time.Sleep(time.Duration(delayMs) * time.Millisecond)
	}
}

// throttleRead applies bandwidth throttling to read operations.
func (c *PacketConn) throttleRead(n int) {
	if c.opts.MaxReadBandwidth <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Refill tokens based on time elapsed
	now := time.Now()
	elapsed := now.Sub(c.lastReadRefill).Seconds()
	c.readTokens += elapsed * float64(c.opts.MaxReadBandwidth)
	if c.readTokens > float64(c.opts.MaxReadBandwidth) {
		c.readTokens = float64(c.opts.MaxReadBandwidth)
	}
	c.lastReadRefill = now

	// Consume tokens
	tokensNeeded := float64(n)
	if c.readTokens < tokensNeeded {
		// Wait until we have enough tokens
		waitTime := (tokensNeeded - c.readTokens) / float64(c.opts.MaxReadBandwidth)
		c.mu.Unlock()
		time.Sleep(time.Duration(waitTime * float64(time.Second)))
		c.mu.Lock()
		c.readTokens = 0
		c.lastReadRefill = time.Now()
	} else {
		c.readTokens -= tokensNeeded
	}
}

// throttleWrite applies bandwidth throttling to write operations.
func (c *PacketConn) throttleWrite(n int) {
	if c.opts.MaxWriteBandwidth <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Refill tokens based on time elapsed
	now := time.Now()
	elapsed := now.Sub(c.lastWriteRefill).Seconds()
	c.writeTokens += elapsed * float64(c.opts.MaxWriteBandwidth)
	if c.writeTokens > float64(c.opts.MaxWriteBandwidth) {
		c.writeTokens = float64(c.opts.MaxWriteBandwidth)
	}
	c.lastWriteRefill = now

	// Consume tokens
	tokensNeeded := float64(n)
	if c.writeTokens < tokensNeeded {
		// Wait until we have enough tokens
		waitTime := (tokensNeeded - c.writeTokens) / float64(c.opts.MaxWriteBandwidth)
		c.mu.Unlock()
		time.Sleep(time.Duration(waitTime * float64(time.Second)))
		c.mu.Lock()
		c.writeTokens = 0
		c.lastWriteRefill = time.Now()
	} else {
		c.writeTokens -= tokensNeeded
	}
}

// ReadFromUDPAddrPort reads a packet from the connection with disruption applied.
func (c *PacketConn) ReadFromUDPAddrPort(b []byte) (n int, addr netip.AddrPort, err error) {
	// Apply read delay first
	c.applyDelay(c.opts.ReadDelayMs)

	// Check if we should drop the packet
	if c.opts.ReadDropRate.ShouldDisrupt() {
		// Drop by reading but returning error
		_, _, _ = c.underlying.ReadFromUDPAddrPort(b)
		return 0, netip.AddrPort{}, ErrPacketDropped
	}

	// Check if we should error
	if c.opts.ReadErrorRate.ShouldDisrupt() {
		return 0, netip.AddrPort{}, ErrDisruptionError
	}

	// Perform actual read
	n, addr, err = c.underlying.ReadFromUDPAddrPort(b)
	if err != nil {
		return n, addr, err
	}

	// Apply bandwidth throttling after successful read
	c.throttleRead(n)

	return n, addr, nil
}

// WriteToUDPAddrPort writes a packet to the connection with disruption applied.
func (c *PacketConn) WriteToUDPAddrPort(b []byte, addr netip.AddrPort) (n int, err error) {
	// Apply write delay first
	c.applyDelay(c.opts.WriteDelayMs)

	// Check if we should drop the packet
	if c.opts.WriteDropRate.ShouldDisrupt() {
		// Pretend we wrote it but don't actually send
		return len(b), nil
	}

	// Check if we should error
	if c.opts.WriteErrorRate.ShouldDisrupt() {
		return 0, ErrDisruptionError
	}

	// Apply bandwidth throttling before write
	c.throttleWrite(len(b))

	// Perform actual write
	return c.underlying.WriteToUDPAddrPort(b, addr)
}

// Close closes the underlying connection.
func (c *PacketConn) Close() error {
	return c.underlying.Close()
}

// LocalAddr returns the local network address.
func (c *PacketConn) LocalAddr() net.Addr {
	return c.underlying.LocalAddr()
}

// SetDeadline sets the read and write deadlines.
func (c *PacketConn) SetDeadline(t time.Time) error {
	return c.underlying.SetDeadline(t)
}

// SetReadDeadline sets the deadline for future Read calls.
func (c *PacketConn) SetReadDeadline(t time.Time) error {
	return c.underlying.SetReadDeadline(t)
}

// SetWriteDeadline sets the deadline for future Write calls.
func (c *PacketConn) SetWriteDeadline(t time.Time) error {
	return c.underlying.SetWriteDeadline(t)
}
