# netdisrupt

Package `netdisrupt` provides network disruption capabilities for testing network applications under adverse conditions.

## Overview

This package allows you to wrap network connections with configurable disruption parameters to simulate real-world network conditions such as:

- Packet loss (read/write drops)
- Network errors
- Bandwidth throttling
- Latency/delays

## Usage

### Using Predefined Profiles

The package includes several predefined network profiles that simulate common mobile network conditions:

```go
import "tailscale.com/net/netdisrupt"

// Wrap your PacketConn with a disruption profile
disruptedConn := netdisrupt.WrapPacketConn(conn, netdisrupt.DisruptionProfile3G)

// Use the connection normally - it will simulate 3G conditions
n, addr, err := disruptedConn.ReadFromUDPAddrPort(buffer)
```

### Available Profiles

- `DisruptionProfileNone` - No disruption (normal operation)
- `DisruptionProfile2GEdge` - Poor 2G EDGE connection (~100-150 Kbps, high latency, 8% packet loss)
- `DisruptionProfile3G` - Typical 3G connection (~384 Kbps, moderate latency, 3% packet loss)
- `DisruptionProfileLTE` - Degraded LTE connection (~5 Mbps, low latency, 1% packet loss)
- `DisruptionProfileHighLatency` - Good bandwidth but high latency (~2 Mbps, 500ms latency)
- `DisruptionProfileUnstable` - Very unstable connection (high packet loss, variable conditions)

### Custom Profiles

You can create custom disruption profiles:

```go
customProfile := &netdisrupt.DisruptionOptions{
    ReadDropRate:      netdisrupt.MustDisruptionRate(0.05),  // 5% packet drop
    WriteDropRate:     netdisrupt.MustDisruptionRate(0.05),
    ReadErrorRate:     netdisrupt.MustDisruptionRate(0.01),  // 1% error rate
    WriteErrorRate:    netdisrupt.MustDisruptionRate(0.01),
    MaxReadBandwidth:  100 * 1024,  // 100 KB/s
    MaxWriteBandwidth: 100 * 1024,
    ReadDelayMs:       50,  // 50ms latency
    WriteDelayMs:      50,
}

disruptedConn := netdisrupt.WrapPacketConn(conn, customProfile)
```

## Disruption Parameters

### Packet Loss (Drop Rates)

- `ReadDropRate` / `WriteDropRate`: Probability (0-1) that a packet will be silently dropped
- When a packet is dropped, `ErrPacketDropped` is returned

### Error Injection

- `ReadErrorRate` / `WriteErrorRate`: Probability (0-1) that an operation will fail with an error
- When triggered, `ErrDisruptionError` is returned

### Bandwidth Throttling

- `MaxReadBandwidth` / `MaxWriteBandwidth`: Maximum throughput in bytes per second (0 = unlimited)
- Uses token bucket algorithm for smooth rate limiting
- Automatically adds delays when bandwidth limit is exceeded

### Latency

- `ReadDelayMs` / `WriteDelayMs`: Additional delay in milliseconds added to each operation
- Simulates network round-trip time

## Example: Testing with Network Disruption

```go
func TestMyProtocolUnder3G(t *testing.T) {
    // Create your connection
    conn, err := net.ListenPacket("udp", "127.0.0.1:0")
    if err != nil {
        t.Fatal(err)
    }
    defer conn.Close()

    // Wrap with 3G network conditions
    disruptedConn := netdisrupt.WrapPacketConn(
        conn.(nettype.PacketConn),
        netdisrupt.DisruptionProfile3G,
    )

    // Run your tests using disruptedConn
    // Your code will experience 3G network conditions
    testMyProtocol(t, disruptedConn)
}
```

## Integration with Tailscale

This package is designed to work with `nettype.PacketConn` interfaces used throughout Tailscale. You can wrap any `PacketConn` implementation, including:

- `*net.UDPConn` (after casting to `nettype.PacketConn`)
- `magicsock.RebindingUDPConn`
- `memnet.Conn`
- Any custom `PacketConn` implementation

## Performance Considerations

- Bandwidth throttling uses a token bucket algorithm which adds minimal overhead
- Delays are implemented with `time.Sleep()` and are accurate to the system scheduler granularity
- Random number generation for drop/error rates uses `math/rand/v2` for good performance
- The wrapper is safe for concurrent use (thread-safe)

## Implementation Details

The disruption wrapper operates on each packet individually:

1. **Delay** is applied first (simulating network propagation time)
2. **Drop/Error checks** are performed (probabilistic)
3. **Actual I/O** operation occurs
4. **Bandwidth throttling** is applied after successful I/O

This ordering ensures realistic behavior where latency occurs before the decision of whether a packet succeeds or fails.
