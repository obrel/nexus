package snowflake

import (
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/sony/sonyflake"
)

// Generator wraps Sonyflake for globally unique, time-ordered 64-bit ID generation.
type Generator struct {
	sf *sonyflake.Sonyflake
}

// New creates a new Generator. The machine ID is derived from the lower 16 bits
// of the machine's primary IP address, ensuring uniqueness across nodes.
func New() (*Generator, error) {
	st := sonyflake.Settings{
		StartTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		MachineID: lowerIPv4,
	}

	sf := sonyflake.NewSonyflake(st)
	if sf == nil {
		return nil, fmt.Errorf("sonyflake: failed to initialize (invalid settings or machine ID)")
	}

	return &Generator{sf: sf}, nil
}

// NextID returns the next unique 64-bit Snowflake ID.
func (g *Generator) NextID() (uint64, error) {
	id, err := g.sf.NextID()
	if err != nil {
		return 0, fmt.Errorf("sonyflake: failed to generate ID: %w", err)
	}
	return id, nil
}

// lowerIPv4 resolves the machine's primary non-loopback IPv4 address and returns
// the lower 16 bits as the machine ID (0–65535).
func lowerIPv4() (uint16, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return 0, err
	}

	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}

		if ip == nil || ip.IsLoopback() {
			continue
		}

		ip = ip.To4()
		if ip == nil {
			continue
		}

		// Lower 16 bits: ip[2]<<8 | ip[3]
		return uint16(ip[2])<<8 | uint16(ip[3]), nil
	}

	// Fallback 1: explicit override via environment variable
	if envID := os.Getenv("MACHINE_ID"); envID != "" {
		if id, err := strconv.ParseUint(envID, 10, 16); err == nil {
			return uint16(id), nil
		}
	}

	// Fallback 2: hash the hostname for a stable, node-unique value
	if hostname, err := os.Hostname(); err == nil {
		h := fnv.New32a()
		h.Write([]byte(hostname))
		return uint16(h.Sum32()), nil
	}

	// Last resort: time-based pseudo-random (single-node / test environments)
	return uint16(time.Now().UnixNano()), nil
}
