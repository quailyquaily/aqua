package maep

import (
	"testing"
	"time"
)

func TestNodeAllowDataPushPerMinute(t *testing.T) {
	n := &Node{
		opts: NodeOptions{
			DataPushPerMinute: 2,
		},
		pushRateWindows: map[string]pushRateWindow{},
	}
	now := time.Date(2026, 2, 6, 12, 30, 10, 0, time.UTC)
	if !n.allowDataPush("peer-a", now) {
		t.Fatalf("first request should pass")
	}
	if !n.allowDataPush("peer-a", now.Add(10*time.Second)) {
		t.Fatalf("second request should pass")
	}
	if n.allowDataPush("peer-a", now.Add(20*time.Second)) {
		t.Fatalf("third request in same minute should be rate limited")
	}
	if !n.allowDataPush("peer-a", now.Add(1*time.Minute)) {
		t.Fatalf("request in next minute should pass")
	}
}
