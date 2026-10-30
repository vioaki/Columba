package server

import (
	"os"
	"testing"

	"columba.local/backend/internal/outbound"
)

// Mock upstreams are isolated loopback listeners.
func TestMain(m *testing.M) {
	outbound.SetAllowPrivateDial(true)
	os.Exit(m.Run())
}
