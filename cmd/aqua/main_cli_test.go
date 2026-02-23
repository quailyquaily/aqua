package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/aqua/aqua"
)

func TestResolvePushMessage(t *testing.T) {
	t.Parallel()

	t.Run("uses positional message", func(t *testing.T) {
		t.Parallel()

		msg, err := resolvePushMessage("", []string{"peerA", "hello"})
		if err != nil {
			t.Fatalf("resolvePushMessage() error = %v", err)
		}
		if msg != "hello" {
			t.Fatalf("message mismatch: got %q want %q", msg, "hello")
		}
	})

	t.Run("uses flag message", func(t *testing.T) {
		t.Parallel()

		msg, err := resolvePushMessage("  from-flag  ", []string{"peerA"})
		if err != nil {
			t.Fatalf("resolvePushMessage() error = %v", err)
		}
		if msg != "from-flag" {
			t.Fatalf("message mismatch: got %q want %q", msg, "from-flag")
		}
	})

	t.Run("rejects duplicate message sources", func(t *testing.T) {
		t.Parallel()

		_, err := resolvePushMessage("from-flag", []string{"peerA", "from-arg"})
		if err == nil {
			t.Fatalf("expected duplicate source error")
		}
		if !strings.Contains(err.Error(), "both positional argument and --message") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects missing message", func(t *testing.T) {
		t.Parallel()

		_, err := resolvePushMessage("", []string{"peerA"})
		if err == nil {
			t.Fatalf("expected missing message error")
		}
	})
}

func TestExtractPeerIDFromDialAddress(t *testing.T) {
	t.Parallel()

	identity, err := aqua.GenerateIdentity(time.Date(2026, 2, 22, 7, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}
	valid := fmt.Sprintf("/ip4/127.0.0.1/tcp/6372/p2p/%s", identity.PeerID)

	t.Run("extracts peer id", func(t *testing.T) {
		t.Parallel()

		peerID, err := extractPeerIDFromDialAddress(valid)
		if err != nil {
			t.Fatalf("extractPeerIDFromDialAddress() error = %v", err)
		}
		if peerID != identity.PeerID {
			t.Fatalf("peer_id mismatch: got %q want %q", peerID, identity.PeerID)
		}
	})

	t.Run("rejects missing p2p", func(t *testing.T) {
		t.Parallel()

		_, err := extractPeerIDFromDialAddress("/ip4/127.0.0.1/tcp/6372")
		if err == nil {
			t.Fatalf("expected missing /p2p error")
		}
	})

	t.Run("rejects invalid peer id", func(t *testing.T) {
		t.Parallel()

		_, err := extractPeerIDFromDialAddress("/ip4/127.0.0.1/tcp/6372/p2p/not-a-peer-id")
		if err == nil {
			t.Fatalf("expected invalid peer id error")
		}
	})
}

func TestContactDisplayLabel(t *testing.T) {
	t.Parallel()

	t.Run("prefers display name", func(t *testing.T) {
		t.Parallel()

		label := contactDisplayLabel(aqua.Contact{
			DisplayName: "  teammate  ",
			Nickname:    "alice",
		})
		if label != "teammate" {
			t.Fatalf("label mismatch: got %q want %q", label, "teammate")
		}
	})

	t.Run("falls back to nickname", func(t *testing.T) {
		t.Parallel()

		label := contactDisplayLabel(aqua.Contact{Nickname: "  alice  "})
		if label != "alice" {
			t.Fatalf("label mismatch: got %q want %q", label, "alice")
		}
	})

	t.Run("returns empty when both are missing", func(t *testing.T) {
		t.Parallel()

		label := contactDisplayLabel(aqua.Contact{})
		if label != "" {
			t.Fatalf("expected empty label, got %q", label)
		}
	})
}

func TestSplitDirectAndRelayAddresses(t *testing.T) {
	t.Parallel()

	addresses := []string{
		"/ip4/127.0.0.1/tcp/6372/p2p/12D3KooWDirectPeer",
		"/dns4/relay.example.com/tcp/6372/p2p/12D3KooWRelayPeer/p2p-circuit/p2p/12D3KooWTargetPeer",
	}
	direct, relay := splitDirectAndRelayAddresses(addresses)
	if len(direct) != 1 {
		t.Fatalf("direct length mismatch: got %d want 1", len(direct))
	}
	if len(relay) != 1 {
		t.Fatalf("relay length mismatch: got %d want 1", len(relay))
	}
}

func TestBuildRelayAdvertiseAddresses(t *testing.T) {
	t.Parallel()

	relayIdentity, err := aqua.GenerateIdentity(time.Date(2026, 2, 22, 13, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(relay) error = %v", err)
	}
	localIdentity, err := aqua.GenerateIdentity(time.Date(2026, 2, 22, 13, 0, 1, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(local) error = %v", err)
	}

	relayEndpoint := fmt.Sprintf("/ip4/127.0.0.1/tcp/6372/p2p/%s", relayIdentity.PeerID)
	addresses, err := buildRelayAdvertiseAddresses([]string{relayEndpoint}, localIdentity.PeerID)
	if err != nil {
		t.Fatalf("buildRelayAdvertiseAddresses() error = %v", err)
	}
	if len(addresses) != 1 {
		t.Fatalf("address length mismatch: got %d want 1", len(addresses))
	}
	wantSuffix := fmt.Sprintf("/p2p/%s/p2p-circuit/p2p/%s", relayIdentity.PeerID, localIdentity.PeerID)
	if !strings.Contains(addresses[0], wantSuffix) {
		t.Fatalf("relay advertise address mismatch: got %q, want suffix %q", addresses[0], wantSuffix)
	}
}

func TestBuildRelayAdvertiseAddresses_RejectsCircuitInput(t *testing.T) {
	t.Parallel()

	identity, err := aqua.GenerateIdentity(time.Date(2026, 2, 22, 13, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}
	_, err = buildRelayAdvertiseAddresses(
		[]string{"/dns4/relay.example.com/tcp/6372/p2p/12D3KooWRelayPeer/p2p-circuit"},
		identity.PeerID,
	)
	if err == nil {
		t.Fatalf("expected /p2p-circuit input to be rejected")
	}
}
