package maep

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestServiceAuditOnImportAndVerify(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "maep")
	store := NewFileStore(root)
	svc := NewService(store)

	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)
	remoteIdentity, err := GenerateIdentity(now)
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}
	card, err := BuildSignedContactCard(
		remoteIdentity,
		[]string{fmt.Sprintf("/ip4/127.0.0.1/tcp/4102/p2p/%s", remoteIdentity.PeerID)},
		1,
		1,
		now,
		nil,
	)
	if err != nil {
		t.Fatalf("BuildSignedContactCard() error = %v", err)
	}
	rawCard, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("json.Marshal(card) error = %v", err)
	}
	if _, err := svc.ImportContactCard(ctx, rawCard, "remote", now); err != nil {
		t.Fatalf("ImportContactCard() error = %v", err)
	}

	if _, err := svc.MarkContactVerified(ctx, remoteIdentity.PeerID, now.Add(time.Minute)); err != nil {
		t.Fatalf("MarkContactVerified() error = %v", err)
	}

	events, err := svc.ListAuditEvents(ctx, remoteIdentity.PeerID, "", 10)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("ListAuditEvents() length mismatch: got %d want 2", len(events))
	}

	foundImport := false
	foundVerify := false
	for _, event := range events {
		switch event.Action {
		case AuditActionContactImportCreated:
			foundImport = true
			if event.NewTrustState != TrustStateTOFU {
				t.Fatalf("import audit trust state mismatch: got %s want %s", event.NewTrustState, TrustStateTOFU)
			}
		case AuditActionTrustStateChanged:
			foundVerify = true
			if event.PreviousTrustState != TrustStateTOFU {
				t.Fatalf("verify previous trust mismatch: got %s want %s", event.PreviousTrustState, TrustStateTOFU)
			}
			if event.NewTrustState != TrustStateVerified {
				t.Fatalf("verify new trust mismatch: got %s want %s", event.NewTrustState, TrustStateVerified)
			}
			if event.Reason != "manual_verify" {
				t.Fatalf("verify reason mismatch: got %s want manual_verify", event.Reason)
			}
		}
	}
	if !foundImport {
		t.Fatalf("missing %s audit event", AuditActionContactImportCreated)
	}
	if !foundVerify {
		t.Fatalf("missing %s audit event", AuditActionTrustStateChanged)
	}
}
