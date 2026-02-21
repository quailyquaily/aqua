package aqua

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestResolveExplicitDialTarget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 2, 22, 5, 0, 0, 0, time.UTC)
	identityA, err := GenerateIdentity(now)
	if err != nil {
		t.Fatalf("GenerateIdentity(identityA) error = %v", err)
	}
	identityB, err := GenerateIdentity(now.Add(time.Second))
	if err != nil {
		t.Fatalf("GenerateIdentity(identityB) error = %v", err)
	}
	validAddress := fmt.Sprintf("/ip4/127.0.0.1/tcp/4101/p2p/%s", identityA.PeerID)
	mismatchedAddress := fmt.Sprintf("/ip4/127.0.0.1/tcp/4102/p2p/%s", identityB.PeerID)

	t.Run("accepts explicit address set", func(t *testing.T) {
		t.Parallel()

		peerID, addresses, err := resolveExplicitDialTarget(identityA.PeerID, []string{validAddress, validAddress})
		if err != nil {
			t.Fatalf("resolveExplicitDialTarget() error = %v", err)
		}
		if peerID.String() != identityA.PeerID {
			t.Fatalf("peer_id mismatch: got %s want %s", peerID.String(), identityA.PeerID)
		}
		if len(addresses) != 1 || addresses[0] != validAddress {
			t.Fatalf("addresses mismatch: got %v want [%s]", addresses, validAddress)
		}
	})

	t.Run("rejects missing peer_id", func(t *testing.T) {
		t.Parallel()

		if _, _, err := resolveExplicitDialTarget("", []string{validAddress}); err == nil {
			t.Fatalf("expected missing peer_id to fail")
		}
	})

	t.Run("rejects invalid peer_id", func(t *testing.T) {
		t.Parallel()

		if _, _, err := resolveExplicitDialTarget("not-a-peer-id", []string{validAddress}); err == nil {
			t.Fatalf("expected invalid peer_id to fail")
		}
	})

	t.Run("rejects empty addresses", func(t *testing.T) {
		t.Parallel()

		if _, _, err := resolveExplicitDialTarget(identityA.PeerID, nil); err == nil {
			t.Fatalf("expected empty address list to fail")
		}
	})

	t.Run("rejects peer mismatch addresses", func(t *testing.T) {
		t.Parallel()

		if _, _, err := resolveExplicitDialTarget(identityA.PeerID, []string{mismatchedAddress}); err == nil {
			t.Fatalf("expected mismatched peer_id address to fail")
		}
	})
}

func TestNodeGetContactCard_FromUnknownPeer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	svcServer := NewService(NewFileStore(t.TempDir()))
	if _, _, err := svcServer.EnsureIdentity(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("EnsureIdentity(server) error = %v", err)
	}
	serverNode, err := NewNode(ctx, svcServer, NodeOptions{
		ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"},
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
			t.Skipf("network listen unavailable in this environment: %v", err)
		}
		t.Fatalf("NewNode(server) error = %v", err)
	}
	defer serverNode.Close()

	serverAddress := ""
	for _, addr := range serverNode.AddrStrings() {
		if strings.Contains(addr, "/ip4/127.0.0.1/") {
			serverAddress = addr
			break
		}
	}
	if serverAddress == "" {
		addresses := serverNode.AddrStrings()
		if len(addresses) == 0 {
			t.Fatalf("server did not expose any dial address")
		}
		serverAddress = addresses[0]
	}

	svcClient := NewService(NewFileStore(t.TempDir()))
	if _, _, err := svcClient.EnsureIdentity(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("EnsureIdentity(client) error = %v", err)
	}
	clientNode, err := NewNode(ctx, svcClient, NodeOptions{DialOnly: true})
	if err != nil {
		t.Fatalf("NewNode(client) error = %v", err)
	}
	defer clientNode.Close()

	if _, found, err := svcServer.GetContactByPeerID(ctx, clientNode.PeerID()); err != nil {
		t.Fatalf("GetContactByPeerID(server, client) error = %v", err)
	} else if found {
		t.Fatalf("server should not have pre-imported client contact")
	}

	rawCard, err := clientNode.GetContactCard(ctx, serverNode.PeerID(), []string{serverAddress})
	if err != nil {
		t.Fatalf("GetContactCard() error = %v", err)
	}
	parsed, err := ParseAndVerifyContactCard(rawCard, time.Now().UTC())
	if err != nil {
		t.Fatalf("ParseAndVerifyContactCard() error = %v", err)
	}
	if parsed.Card.Payload.PeerID != serverNode.PeerID() {
		t.Fatalf("fetched card peer_id mismatch: got %s want %s", parsed.Card.Payload.PeerID, serverNode.PeerID())
	}
	if len(parsed.Card.Payload.Addresses) != 1 || parsed.Card.Payload.Addresses[0] != serverAddress {
		t.Fatalf("fetched card addresses mismatch: got %v want [%s]", parsed.Card.Payload.Addresses, serverAddress)
	}
}
