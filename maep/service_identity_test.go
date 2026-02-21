package maep

import (
	"context"
	"testing"
	"time"
)

func TestServiceEnsureIdentity_ReusesExistingIdentity(t *testing.T) {
	svc := NewService(NewFileStore(t.TempDir()))
	ctx := context.Background()

	first, created, err := svc.EnsureIdentity(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("EnsureIdentity() first call error = %v", err)
	}
	if !created {
		t.Fatalf("EnsureIdentity() first call expected created=true")
	}

	second, created, err := svc.EnsureIdentity(ctx, time.Now().UTC().Add(time.Second))
	if err != nil {
		t.Fatalf("EnsureIdentity() second call error = %v", err)
	}
	if created {
		t.Fatalf("EnsureIdentity() second call expected created=false")
	}
	if second.PeerID != first.PeerID {
		t.Fatalf("peer_id changed: got %s want %s", second.PeerID, first.PeerID)
	}
	if second.NodeUUID != first.NodeUUID {
		t.Fatalf("node_uuid changed: got %s want %s", second.NodeUUID, first.NodeUUID)
	}
	if second.NodeID != first.NodeID {
		t.Fatalf("node_id changed: got %s want %s", second.NodeID, first.NodeID)
	}
	if second.IdentityPubEd25519 != first.IdentityPubEd25519 {
		t.Fatalf("identity_pub_ed25519 changed")
	}
}
