package aqua

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestDialAddrInfoForTarget_Direct(t *testing.T) {
	t.Parallel()

	targetIdentity, err := GenerateIdentity(time.Date(2026, 2, 22, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(target) error = %v", err)
	}
	targetPeerID, err := peer.Decode(targetIdentity.PeerID)
	if err != nil {
		t.Fatalf("peer.Decode(target) error = %v", err)
	}
	raw := fmt.Sprintf("/ip4/127.0.0.1/tcp/6371/p2p/%s", targetIdentity.PeerID)

	info, err := dialAddrInfoForTarget(raw, targetPeerID)
	if err != nil {
		t.Fatalf("dialAddrInfoForTarget(direct) error = %v", err)
	}
	if info.ID != targetPeerID {
		t.Fatalf("peer id mismatch: got %s want %s", info.ID.String(), targetPeerID.String())
	}
	if len(info.Addrs) != 1 {
		t.Fatalf("addr count mismatch: got %d want 1", len(info.Addrs))
	}
	if got := info.Addrs[0].String(); got != "/ip4/127.0.0.1/tcp/6371" {
		t.Fatalf("direct addr mismatch: got %q", got)
	}
}

func TestDialAddrInfoForTarget_RelayCircuit(t *testing.T) {
	t.Parallel()

	relayIdentity, err := GenerateIdentity(time.Date(2026, 2, 22, 11, 0, 1, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(relay) error = %v", err)
	}
	targetIdentity, err := GenerateIdentity(time.Date(2026, 2, 22, 11, 0, 2, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(target) error = %v", err)
	}
	targetPeerID, err := peer.Decode(targetIdentity.PeerID)
	if err != nil {
		t.Fatalf("peer.Decode(target) error = %v", err)
	}

	raw := fmt.Sprintf(
		"/ip4/18.179.41.50/tcp/6372/p2p/%s/p2p-circuit/p2p/%s",
		relayIdentity.PeerID,
		targetIdentity.PeerID,
	)
	info, err := dialAddrInfoForTarget(raw, targetPeerID)
	if err != nil {
		t.Fatalf("dialAddrInfoForTarget(relay) error = %v", err)
	}
	if info.ID != targetPeerID {
		t.Fatalf("peer id mismatch: got %s want %s", info.ID.String(), targetPeerID.String())
	}
	if len(info.Addrs) != 1 {
		t.Fatalf("addr count mismatch: got %d want 1", len(info.Addrs))
	}
	wantAddr := fmt.Sprintf("/ip4/18.179.41.50/tcp/6372/p2p/%s/p2p-circuit", relayIdentity.PeerID)
	if got := info.Addrs[0].String(); got != wantAddr {
		t.Fatalf("relay addr mismatch: got %q want %q", got, wantAddr)
	}
}

func TestDialAddrInfoForTarget_RejectsMismatchedPeerID(t *testing.T) {
	t.Parallel()

	targetIdentity, err := GenerateIdentity(time.Date(2026, 2, 22, 11, 0, 3, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(target) error = %v", err)
	}
	otherIdentity, err := GenerateIdentity(time.Date(2026, 2, 22, 11, 0, 4, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(other) error = %v", err)
	}
	targetPeerID, err := peer.Decode(targetIdentity.PeerID)
	if err != nil {
		t.Fatalf("peer.Decode(target) error = %v", err)
	}

	raw := fmt.Sprintf("/ip4/127.0.0.1/tcp/6371/p2p/%s", otherIdentity.PeerID)
	_, err = dialAddrInfoForTarget(raw, targetPeerID)
	if err == nil {
		t.Fatalf("expected mismatched peer id to fail")
	}
	if !strings.Contains(err.Error(), "targets") {
		t.Fatalf("unexpected error: %v", err)
	}
}
