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

func TestServiceSetIdentityNickname(t *testing.T) {
	svc := NewService(NewFileStore(t.TempDir()))
	ctx := context.Background()

	identity, _, err := svc.EnsureIdentity(ctx, time.Date(2026, 2, 22, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("EnsureIdentity() error = %v", err)
	}

	updatedAt := identity.UpdatedAt
	updated, err := svc.SetIdentityNickname(ctx, "  alice  ", time.Date(2026, 2, 22, 9, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SetIdentityNickname() error = %v", err)
	}
	if updated.Nickname != "alice" {
		t.Fatalf("nickname mismatch: got %q want %q", updated.Nickname, "alice")
	}
	if !updated.UpdatedAt.After(updatedAt) {
		t.Fatalf("updated_at should move forward")
	}

	same, err := svc.SetIdentityNickname(ctx, "alice", time.Date(2026, 2, 22, 9, 6, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SetIdentityNickname(same) error = %v", err)
	}
	if !same.UpdatedAt.Equal(updated.UpdatedAt) {
		t.Fatalf("updated_at should not change when nickname is unchanged: got %s want %s", same.UpdatedAt, updated.UpdatedAt)
	}

	cleared, err := svc.SetIdentityNickname(ctx, "   ", time.Date(2026, 2, 22, 9, 7, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SetIdentityNickname(clear) error = %v", err)
	}
	if cleared.Nickname != "" {
		t.Fatalf("nickname should be cleared, got %q", cleared.Nickname)
	}
}

func TestServiceSetIdentityNickname_IdentityMissing(t *testing.T) {
	svc := NewService(NewFileStore(t.TempDir()))
	ctx := context.Background()
	if _, err := svc.SetIdentityNickname(ctx, "alice", time.Now().UTC()); err == nil {
		t.Fatalf("expected SetIdentityNickname() to fail when identity is missing")
	}
}
