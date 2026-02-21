package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/aqua/maep"
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

	identity, err := maep.GenerateIdentity(time.Date(2026, 2, 22, 7, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}
	valid := fmt.Sprintf("/ip4/127.0.0.1/tcp/6371/p2p/%s", identity.PeerID)

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

		_, err := extractPeerIDFromDialAddress("/ip4/127.0.0.1/tcp/6371")
		if err == nil {
			t.Fatalf("expected missing /p2p error")
		}
	})

	t.Run("rejects invalid peer id", func(t *testing.T) {
		t.Parallel()

		_, err := extractPeerIDFromDialAddress("/ip4/127.0.0.1/tcp/6371/p2p/not-a-peer-id")
		if err == nil {
			t.Fatalf("expected invalid peer id error")
		}
	})
}
