package main

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/quailyquaily/aqua/aqua"
)

func TestParseRelayAllowlist(t *testing.T) {
	t.Parallel()

	idA, err := aqua.GenerateIdentity(time.Date(2026, 2, 22, 14, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(idA) error = %v", err)
	}
	idB, err := aqua.GenerateIdentity(time.Date(2026, 2, 22, 14, 0, 1, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(idB) error = %v", err)
	}

	allow, err := parseRelayAllowlist([]string{idA.PeerID, idB.PeerID, idA.PeerID})
	if err != nil {
		t.Fatalf("parseRelayAllowlist() error = %v", err)
	}
	if len(allow) != 2 {
		t.Fatalf("allowlist size mismatch: got %d want 2", len(allow))
	}

	empty, err := parseRelayAllowlist(nil)
	if err != nil {
		t.Fatalf("parseRelayAllowlist(nil) error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty allowlist, got %d", len(empty))
	}
}

func TestParseRelayAllowlist_RejectsInvalidPeerID(t *testing.T) {
	t.Parallel()

	if _, err := parseRelayAllowlist([]string{"not-a-peer-id"}); err == nil {
		t.Fatalf("expected invalid peer id error")
	}
}

func TestRelayAllowlistACL(t *testing.T) {
	t.Parallel()

	srcIdentity, err := aqua.GenerateIdentity(time.Date(2026, 2, 22, 14, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(src) error = %v", err)
	}
	destIdentity, err := aqua.GenerateIdentity(time.Date(2026, 2, 22, 14, 1, 1, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(dest) error = %v", err)
	}
	otherIdentity, err := aqua.GenerateIdentity(time.Date(2026, 2, 22, 14, 1, 2, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(other) error = %v", err)
	}
	srcID, err := peer.Decode(srcIdentity.PeerID)
	if err != nil {
		t.Fatalf("peer.Decode(src) error = %v", err)
	}
	destID, err := peer.Decode(destIdentity.PeerID)
	if err != nil {
		t.Fatalf("peer.Decode(dest) error = %v", err)
	}
	otherID, err := peer.Decode(otherIdentity.PeerID)
	if err != nil {
		t.Fatalf("peer.Decode(other) error = %v", err)
	}

	allowAll := relayAllowlistACL{allowed: map[peer.ID]bool{}}
	if !allowAll.AllowReserve(srcID, nil) {
		t.Fatalf("expected allow-all reserve to pass")
	}
	if !allowAll.AllowConnect(srcID, nil, destID) {
		t.Fatalf("expected allow-all connect to pass")
	}

	allowMap, err := parseRelayAllowlist([]string{srcIdentity.PeerID, destIdentity.PeerID})
	if err != nil {
		t.Fatalf("parseRelayAllowlist() error = %v", err)
	}
	acl := relayAllowlistACL{allowed: allowMap}
	if !acl.AllowReserve(srcID, nil) {
		t.Fatalf("expected allowed source reserve to pass")
	}
	if acl.AllowReserve(otherID, nil) {
		t.Fatalf("expected non-allowlisted reserve to fail")
	}
	if !acl.AllowConnect(srcID, nil, destID) {
		t.Fatalf("expected allowlisted connect to pass")
	}
	if acl.AllowConnect(srcID, nil, otherID) {
		t.Fatalf("expected connect to non-allowlisted destination to fail")
	}
}
