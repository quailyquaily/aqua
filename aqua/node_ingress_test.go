package aqua

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEnqueueIncomingPushQueueFullReturnsBusy(t *testing.T) {
	t.Parallel()

	n := &Node{
		opts:          NodeOptions{},
		incomingQueue: make(chan incomingPushEnvelope, 1),
	}
	n.incomingQueue <- incomingPushEnvelope{message: InboxMessage{MessageID: "msg-1"}}

	err := n.enqueueIncomingPush(incomingPushEnvelope{message: InboxMessage{MessageID: "msg-2"}})
	if err == nil {
		t.Fatalf("expected enqueue to fail when queue is full")
	}
	if SymbolOf(err) != ErrBusySymbol {
		t.Fatalf("expected ErrBusySymbol, got %q (%v)", SymbolOf(err), err)
	}
}

func TestEnqueueIncomingPushClosedQueueReturnsBusy(t *testing.T) {
	t.Parallel()

	n := &Node{
		opts:          NodeOptions{},
		incomingQueue: make(chan incomingPushEnvelope, 1),
	}
	close(n.incomingQueue)

	err := n.enqueueIncomingPush(incomingPushEnvelope{message: InboxMessage{MessageID: "msg-1"}})
	if err == nil {
		t.Fatalf("expected enqueue to fail for closed queue")
	}
	if SymbolOf(err) != ErrBusySymbol {
		t.Fatalf("expected ErrBusySymbol, got %q (%v)", SymbolOf(err), err)
	}
}

func TestReserveIncomingDedupeRuntime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 23, 3, 0, 0, 0, time.UTC)
	n := &Node{
		opts: NodeOptions{
			DedupeTTL:        time.Minute,
			DedupeMaxEntries: 10,
		},
		incomingDedupeCache: map[string]time.Time{},
	}

	key, deduped := n.reserveIncomingDedupe("peer-a", "chat.message", "idem-1", now)
	if deduped {
		t.Fatalf("first reserve should not be deduped")
	}
	if key == "" {
		t.Fatalf("reserve should return a non-empty key")
	}

	_, deduped = n.reserveIncomingDedupe("peer-a", "chat.message", "idem-1", now.Add(10*time.Second))
	if !deduped {
		t.Fatalf("second reserve in ttl window should be deduped")
	}
}

func TestMakeRPCErrorBusyCode(t *testing.T) {
	t.Parallel()

	raw, err := makeRPCError("req-1", ErrBusySymbol, "incoming queue is full")
	if err != nil {
		t.Fatalf("makeRPCError() error = %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode rpc error json failed: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("error object missing: %#v", resp)
	}
	code, ok := errObj["code"].(float64)
	if !ok {
		t.Fatalf("error code missing: %#v", errObj)
	}
	if int(code) != -32010 {
		t.Fatalf("error code mismatch: got %d want -32010", int(code))
	}
}
