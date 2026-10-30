package server

import (
	"testing"
	"time"
)

func TestUsageRequestIDUniqueForSameNanosecond(t *testing.T) {
	now := time.Now()
	firstID, secondID := usageRequestID(now), usageRequestID(now)
	if firstID == secondID {
		t.Fatal("usage IDs must carry a random suffix")
	}
}

func intPtr(v int) *int {
	return &v
}
