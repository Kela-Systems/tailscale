// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package netdisrupt

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"tailscale.com/types/nettype"
)

// mockPacketConn implements nettype.PacketConn for testing.
type mockPacketConn struct {
	readData  []byte
	readAddr  netip.AddrPort
	writeData [][]byte
	writeAddr []netip.AddrPort
}

func (m *mockPacketConn) ReadFromUDPAddrPort(b []byte) (int, netip.AddrPort, error) {
	n := copy(b, m.readData)
	return n, m.readAddr, nil
}

func (m *mockPacketConn) WriteToUDPAddrPort(b []byte, addr netip.AddrPort) (int, error) {
	m.writeData = append(m.writeData, append([]byte(nil), b...))
	m.writeAddr = append(m.writeAddr, addr)
	return len(b), nil
}

func (m *mockPacketConn) Close() error                       { return nil }
func (m *mockPacketConn) LocalAddr() net.Addr                { return &net.UDPAddr{} }
func (m *mockPacketConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockPacketConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockPacketConn) SetWriteDeadline(t time.Time) error { return nil }

func TestDisruptionRate(t *testing.T) {
	// Test valid rates
	rate, err := NewDisruptionRate(0.5)
	if err != nil {
		t.Fatalf("NewDisruptionRate(0.5) failed: %v", err)
	}
	if rate.rate != 0.5 {
		t.Errorf("rate = %f, want 0.5", rate.rate)
	}

	// Test invalid rates
	_, err = NewDisruptionRate(-0.1)
	if err == nil {
		t.Error("NewDisruptionRate(-0.1) should have failed")
	}

	_, err = NewDisruptionRate(1.1)
	if err == nil {
		t.Error("NewDisruptionRate(1.1) should have failed")
	}

	// Test MustDisruptionRate panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustDisruptionRate(2.0) should have panicked")
		}
	}()
	MustDisruptionRate(2.0)
}

func TestDisruptionRateShouldDisrupt(t *testing.T) {
	// Test with rate 0 (should never disrupt)
	rate := MustDisruptionRate(0)
	for i := 0; i < 100; i++ {
		if rate.ShouldDisrupt() {
			t.Error("rate 0 should never disrupt")
		}
	}

	// Test with rate 1 (should always disrupt)
	rate = MustDisruptionRate(1.0)
	for i := 0; i < 100; i++ {
		if !rate.ShouldDisrupt() {
			t.Error("rate 1.0 should always disrupt")
		}
	}

	// Test with rate 0.5 (should disrupt approximately 50% of the time)
	rate = MustDisruptionRate(0.5)
	disruptCount := 0
	iterations := 1000
	for i := 0; i < iterations; i++ {
		if rate.ShouldDisrupt() {
			disruptCount++
		}
	}
	// Allow for some variance (between 40% and 60%)
	if disruptCount < iterations*4/10 || disruptCount > iterations*6/10 {
		t.Errorf("rate 0.5 disrupted %d/%d times, expected around 500", disruptCount, iterations)
	}
}

func TestPacketConnReadDrop(t *testing.T) {
	mock := &mockPacketConn{
		readData: []byte("hello"),
		readAddr: netip.MustParseAddrPort("127.0.0.1:8080"),
	}

	opts := &DisruptionOptions{
		ReadDropRate: MustDisruptionRate(1.0), // Always drop
	}

	conn := WrapPacketConn(mock, opts)

	buf := make([]byte, 100)
	_, _, err := conn.ReadFromUDPAddrPort(buf)
	if err != ErrPacketDropped {
		t.Errorf("expected ErrPacketDropped, got %v", err)
	}
}

func TestPacketConnReadError(t *testing.T) {
	mock := &mockPacketConn{
		readData: []byte("hello"),
		readAddr: netip.MustParseAddrPort("127.0.0.1:8080"),
	}

	opts := &DisruptionOptions{
		ReadErrorRate: MustDisruptionRate(1.0), // Always error
	}

	conn := WrapPacketConn(mock, opts)

	buf := make([]byte, 100)
	_, _, err := conn.ReadFromUDPAddrPort(buf)
	if err != ErrDisruptionError {
		t.Errorf("expected ErrDisruptionError, got %v", err)
	}
}

func TestPacketConnWriteDrop(t *testing.T) {
	mock := &mockPacketConn{}

	opts := &DisruptionOptions{
		WriteDropRate: MustDisruptionRate(1.0), // Always drop
	}

	conn := WrapPacketConn(mock, opts)

	data := []byte("hello")
	addr := netip.MustParseAddrPort("127.0.0.1:8080")
	n, err := conn.WriteToUDPAddrPort(data, addr)
	if err != nil {
		t.Fatalf("WriteToUDPAddrPort failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("wrote %d bytes, want %d", n, len(data))
	}

	// Verify nothing was actually written to underlying connection
	if len(mock.writeData) != 0 {
		t.Error("data should have been dropped, but was written to underlying connection")
	}
}

func TestPacketConnWriteError(t *testing.T) {
	mock := &mockPacketConn{}

	opts := &DisruptionOptions{
		WriteErrorRate: MustDisruptionRate(1.0), // Always error
	}

	conn := WrapPacketConn(mock, opts)

	data := []byte("hello")
	addr := netip.MustParseAddrPort("127.0.0.1:8080")
	_, err := conn.WriteToUDPAddrPort(data, addr)
	if err != ErrDisruptionError {
		t.Errorf("expected ErrDisruptionError, got %v", err)
	}
}

func TestPacketConnDelay(t *testing.T) {
	mock := &mockPacketConn{
		readData: []byte("hello"),
		readAddr: netip.MustParseAddrPort("127.0.0.1:8080"),
	}

	opts := &DisruptionOptions{
		ReadDelayMs:  100,
		WriteDelayMs: 50,
	}

	conn := WrapPacketConn(mock, opts)

	// Test read delay
	start := time.Now()
	buf := make([]byte, 100)
	_, _, err := conn.ReadFromUDPAddrPort(buf)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ReadFromUDPAddrPort failed: %v", err)
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("read delay was %v, expected at least 100ms", elapsed)
	}

	// Test write delay
	start = time.Now()
	data := []byte("world")
	addr := netip.MustParseAddrPort("127.0.0.1:8080")
	_, err = conn.WriteToUDPAddrPort(data, addr)
	elapsed = time.Since(start)
	if err != nil {
		t.Fatalf("WriteToUDPAddrPort failed: %v", err)
	}
	if elapsed < 50*time.Millisecond {
		t.Errorf("write delay was %v, expected at least 50ms", elapsed)
	}
}

func TestPacketConnBandwidthThrottling(t *testing.T) {
	mock := &mockPacketConn{
		readData: make([]byte, 10000), // 10KB
		readAddr: netip.MustParseAddrPort("127.0.0.1:8080"),
	}

	// Set bandwidth limit to 1KB/s
	opts := &DisruptionOptions{
		MaxReadBandwidth:  1024,
		MaxWriteBandwidth: 1024,
	}

	conn := WrapPacketConn(mock, opts)

	// Test read throttling - reading 2KB should take at least 1 second
	start := time.Now()
	buf := make([]byte, 2048)
	_, _, err := conn.ReadFromUDPAddrPort(buf)
	if err != nil {
		t.Fatalf("ReadFromUDPAddrPort failed: %v", err)
	}
	_, _, err = conn.ReadFromUDPAddrPort(buf)
	if err != nil {
		t.Fatalf("ReadFromUDPAddrPort failed: %v", err)
	}
	elapsed := time.Since(start)

	// Should take at least 1 second to read 2KB at 1KB/s
	// Allow some margin for scheduling delays
	if elapsed < 900*time.Millisecond {
		t.Errorf("read throttling was too fast: %v, expected at least 900ms", elapsed)
	}
}

func TestPredefinedProfiles(t *testing.T) {
	profiles := map[string]*DisruptionOptions{
		"None":        DisruptionProfileNone,
		"2G Edge":     DisruptionProfile2GEdge,
		"3G":          DisruptionProfile3G,
		"LTE":         DisruptionProfileLTE,
		"HighLatency": DisruptionProfileHighLatency,
		"Unstable":    DisruptionProfileUnstable,
	}

	for name, profile := range profiles {
		t.Run(name, func(t *testing.T) {
			mock := &mockPacketConn{
				readData: []byte("test data"),
				readAddr: netip.MustParseAddrPort("127.0.0.1:8080"),
			}

			conn := WrapPacketConn(mock, profile)

			// Just verify we can create a connection with each profile
			// and perform basic operations
			buf := make([]byte, 100)
			conn.ReadFromUDPAddrPort(buf)

			data := []byte("test")
			addr := netip.MustParseAddrPort("127.0.0.1:9090")
			conn.WriteToUDPAddrPort(data, addr)
		})
	}
}

func TestPacketConnNormalOperation(t *testing.T) {
	mock := &mockPacketConn{
		readData: []byte("hello world"),
		readAddr: netip.MustParseAddrPort("127.0.0.1:8080"),
	}

	// No disruption
	conn := WrapPacketConn(mock, DisruptionProfileNone)

	// Test read
	buf := make([]byte, 100)
	n, addr, err := conn.ReadFromUDPAddrPort(buf)
	if err != nil {
		t.Fatalf("ReadFromUDPAddrPort failed: %v", err)
	}
	if n != len("hello world") {
		t.Errorf("read %d bytes, want %d", n, len("hello world"))
	}
	if string(buf[:n]) != "hello world" {
		t.Errorf("read %q, want %q", buf[:n], "hello world")
	}
	if addr != mock.readAddr {
		t.Errorf("addr = %v, want %v", addr, mock.readAddr)
	}

	// Test write
	data := []byte("test data")
	writeAddr := netip.MustParseAddrPort("127.0.0.1:9090")
	n, err = conn.WriteToUDPAddrPort(data, writeAddr)
	if err != nil {
		t.Fatalf("WriteToUDPAddrPort failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("wrote %d bytes, want %d", n, len(data))
	}

	if len(mock.writeData) != 1 {
		t.Fatalf("writeData has %d entries, want 1", len(mock.writeData))
	}
	if string(mock.writeData[0]) != string(data) {
		t.Errorf("wrote %q, want %q", mock.writeData[0], data)
	}
	if mock.writeAddr[0] != writeAddr {
		t.Errorf("wrote to %v, want %v", mock.writeAddr[0], writeAddr)
	}
}

// Example demonstrates how to use network disruption in tests.
func ExampleWrapPacketConn() {
	// Create a UDP listener
	lc := net.ListenConfig{}
	pc, err := lc.ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer pc.Close()

	// Cast to nettype.PacketConn (in real code, you'd already have this type)
	ntpc := pc.(nettype.PacketConn)

	// Wrap with 3G network conditions
	disruptedConn := WrapPacketConn(ntpc, DisruptionProfile3G)

	// Use disruptedConn as you would normally use a PacketConn
	// It will simulate 3G network conditions (packet loss, latency, bandwidth limits)
	_ = disruptedConn
}

// Example demonstrating custom disruption profile.
func ExampleDisruptionOptions() {
	// Create a custom disruption profile
	customProfile := &DisruptionOptions{
		ReadDropRate:      MustDisruptionRate(0.05), // 5% packet loss
		WriteDropRate:     MustDisruptionRate(0.05), // 5% packet loss
		ReadErrorRate:     MustDisruptionRate(0.01), // 1% errors
		WriteErrorRate:    MustDisruptionRate(0.01), // 1% errors
		MaxReadBandwidth:  100 * 1024,               // 100 KB/s
		MaxWriteBandwidth: 100 * 1024,               // 100 KB/s
		ReadDelayMs:       50,                       // 50ms latency
		WriteDelayMs:      50,                       // 50ms latency
	}

	// Create a mock connection for example purposes
	mock := &mockPacketConn{
		readData: []byte("example"),
		readAddr: netip.MustParseAddrPort("127.0.0.1:8080"),
	}

	disruptedConn := WrapPacketConn(mock, customProfile)
	_ = disruptedConn
}
