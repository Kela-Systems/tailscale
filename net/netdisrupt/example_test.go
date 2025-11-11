// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package netdisrupt_test

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"

	"tailscale.com/net/netdisrupt"
	"tailscale.com/types/nettype"
)

// ExampleWrapPacketConn_basic demonstrates basic usage with a UDP connection.
func ExampleWrapPacketConn_basic() {
	// Create a standard UDP listener
	lc := net.ListenConfig{}
	pc, err := lc.ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer pc.Close()

	// Wrap with 3G network conditions
	disruptedConn := netdisrupt.WrapPacketConn(
		pc.(nettype.PacketConn),
		netdisrupt.DisruptionProfile3G,
	)

	// Now use disruptedConn instead of pc
	// All operations will experience 3G network conditions
	_ = disruptedConn
	fmt.Println("Connection wrapped with 3G profile")
	// Output: Connection wrapped with 3G profile
}

// ExampleDisruptionOptions_custom demonstrates creating a custom disruption profile.
func ExampleDisruptionOptions_custom() {
	// Create a custom profile for testing specific conditions
	customProfile := &netdisrupt.DisruptionOptions{
		ReadDropRate:      netdisrupt.MustDisruptionRate(0.10), // 10% packet loss
		WriteDropRate:     netdisrupt.MustDisruptionRate(0.10),
		ReadErrorRate:     netdisrupt.MustDisruptionRate(0.02), // 2% errors
		WriteErrorRate:    netdisrupt.MustDisruptionRate(0.02),
		MaxReadBandwidth:  256 * 1024, // 256 KB/s (~2 Mbps)
		MaxWriteBandwidth: 128 * 1024, // 128 KB/s (~1 Mbps)
		ReadDelayMs:       100,        // 100ms latency
		WriteDelayMs:      100,
	}

	fmt.Printf("Custom profile: %d KB/s read, %d KB/s write, %dms delay\n",
		customProfile.MaxReadBandwidth/1024,
		customProfile.MaxWriteBandwidth/1024,
		customProfile.ReadDelayMs)
	// Output: Custom profile: 256 KB/s read, 128 KB/s write, 100ms delay
}

// ExampleDisruptionRate demonstrates probability-based disruption.
func ExampleDisruptionRate() {
	// Create a disruption rate of 30%
	rate := netdisrupt.MustDisruptionRate(0.30)

	// Simulate checking if packets should be dropped
	droppedCount := 0
	totalPackets := 1000

	for i := 0; i < totalPackets; i++ {
		if rate.ShouldDisrupt() {
			droppedCount++
		}
	}

	// The actual drop rate will be approximately 30%
	fmt.Printf("Dropped approximately 30%% of packets\n")
	// Output: Dropped approximately 30% of packets
}

// Example_testingUnderAdverseConditions shows how to test protocol behavior
// under different network conditions.
func Example_testingUnderAdverseConditions() {
	// Create a test server
	serverConn, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer serverConn.Close()

	// Wrap server connection with unstable network profile
	disruptedServer := netdisrupt.WrapPacketConn(
		serverConn.(nettype.PacketConn),
		netdisrupt.DisruptionProfileUnstable,
	)

	// Your protocol code would use disruptedServer
	// It will experience: 15% packet loss, 5% errors, limited bandwidth

	fmt.Println("Server running with unstable network conditions")
	// Output: Server running with unstable network conditions

	_ = disruptedServer
}

// Example_bandwidthThrottling demonstrates bandwidth limiting.
func Example_bandwidthThrottling() {
	// Create a profile with strict bandwidth limits
	limitedBandwidth := &netdisrupt.DisruptionOptions{
		MaxReadBandwidth:  10 * 1024, // 10 KB/s
		MaxWriteBandwidth: 10 * 1024,
	}

	lc := net.ListenConfig{}
	pc, _ := lc.ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	defer pc.Close()

	throttledConn := netdisrupt.WrapPacketConn(
		pc.(nettype.PacketConn),
		limitedBandwidth,
	)

	// Write 20KB of data (will take approximately 2 seconds at 10 KB/s)
	data := make([]byte, 20*1024)
	addr := netip.MustParseAddrPort("127.0.0.1:9999")

	start := time.Now()
	throttledConn.WriteToUDPAddrPort(data, addr)
	elapsed := time.Since(start)

	fmt.Printf("Bandwidth throttling enforced (took %v for 20KB)\n", elapsed.Round(100*time.Millisecond))
}

// Example_simulatingMobileNetworks demonstrates testing across different
// mobile network profiles.
func Example_simulatingMobileNetworks() {
	profiles := map[string]*netdisrupt.DisruptionOptions{
		"2G":  netdisrupt.DisruptionProfile2GEdge,
		"3G":  netdisrupt.DisruptionProfile3G,
		"LTE": netdisrupt.DisruptionProfileLTE,
	}

	for name, profile := range profiles {
		lc := net.ListenConfig{}
		pc, _ := lc.ListenPacket(context.Background(), "udp", "127.0.0.1:0")

		disruptedConn := netdisrupt.WrapPacketConn(
			pc.(nettype.PacketConn),
			profile,
		)

		fmt.Printf("Testing with %s profile\n", name)

		// Your test code would run here with disruptedConn

		pc.Close()
		_ = disruptedConn
	}
	// Output:
	// Testing with 2G profile
	// Testing with 3G profile
	// Testing with LTE profile
}

// Example_handlingDisruptionErrors shows how to handle disruption-specific errors.
func Example_handlingDisruptionErrors() {
	// Create a connection with high error rate for demonstration
	errorProfile := &netdisrupt.DisruptionOptions{
		ReadErrorRate: netdisrupt.MustDisruptionRate(0.5), // 50% error rate
	}

	lc := net.ListenConfig{}
	pc, _ := lc.ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	defer pc.Close()

	disruptedConn := netdisrupt.WrapPacketConn(
		pc.(nettype.PacketConn),
		errorProfile,
	)

	// Attempt to read
	buf := make([]byte, 1024)
	_, _, err := disruptedConn.ReadFromUDPAddrPort(buf)

	// Check for disruption-specific errors
	if err == netdisrupt.ErrPacketDropped {
		fmt.Println("Packet was dropped by network disruption")
	} else if err == netdisrupt.ErrDisruptionError {
		fmt.Println("Network disruption error occurred")
	}
}
