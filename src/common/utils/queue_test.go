package utils

import (
	"testing"
	"time"
)

// QueueBackoff thuần túy: lần 1 ~1 phút, trần 30 phút.
func TestQueueBackoffPure(t *testing.T) {
	d1 := time.Until(QueueBackoff(1))
	if d1 < 50*time.Second || d1 > 70*time.Second {
		t.Fatalf("backoff(1) phải ~1m, got %v", d1)
	}
	dMax := time.Until(QueueBackoff(1000))
	if dMax < 29*time.Minute || dMax > 31*time.Minute {
		t.Fatalf("backoff phải trần 30m, got %v", dMax)
	}
}
