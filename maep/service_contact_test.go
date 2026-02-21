package maep

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestServiceImportContactCard_SavesNickname(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "maep")
	svc := NewService(NewFileStore(root))

	now := time.Date(2026, 2, 22, 10, 0, 0, 0, time.UTC)
	remoteIdentity, err := GenerateIdentity(now)
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}
	remoteIdentity.Nickname = "bob"

	card, err := BuildSignedContactCard(
		remoteIdentity,
		[]string{fmt.Sprintf("/ip4/127.0.0.1/tcp/4104/p2p/%s", remoteIdentity.PeerID)},
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

	result, err := svc.ImportContactCard(ctx, rawCard, "", now)
	if err != nil {
		t.Fatalf("ImportContactCard() error = %v", err)
	}
	if result.Contact.Nickname != "bob" {
		t.Fatalf("imported nickname mismatch: got %q want %q", result.Contact.Nickname, "bob")
	}

	persisted, ok, err := svc.GetContactByPeerID(ctx, remoteIdentity.PeerID)
	if err != nil {
		t.Fatalf("GetContactByPeerID() error = %v", err)
	}
	if !ok {
		t.Fatalf("contact not found after import")
	}
	if persisted.Nickname != "bob" {
		t.Fatalf("persisted nickname mismatch: got %q want %q", persisted.Nickname, "bob")
	}
}

func TestServiceImportContactCard_UpdatesNickname(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "maep")
	svc := NewService(NewFileStore(root))

	now := time.Date(2026, 2, 22, 11, 0, 0, 0, time.UTC)
	remoteIdentity, err := GenerateIdentity(now)
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}
	remoteIdentity.Nickname = "bob"

	firstCard, err := BuildSignedContactCard(
		remoteIdentity,
		[]string{fmt.Sprintf("/ip4/127.0.0.1/tcp/4105/p2p/%s", remoteIdentity.PeerID)},
		1,
		1,
		now,
		nil,
	)
	if err != nil {
		t.Fatalf("BuildSignedContactCard(first) error = %v", err)
	}
	firstRaw, err := json.Marshal(firstCard)
	if err != nil {
		t.Fatalf("json.Marshal(first card) error = %v", err)
	}
	if _, err := svc.ImportContactCard(ctx, firstRaw, "remote-alias", now); err != nil {
		t.Fatalf("ImportContactCard(first) error = %v", err)
	}

	remoteIdentity.Nickname = "robert"
	secondCard, err := BuildSignedContactCard(
		remoteIdentity,
		[]string{fmt.Sprintf("/ip4/127.0.0.1/tcp/4105/p2p/%s", remoteIdentity.PeerID)},
		1,
		1,
		now.Add(time.Minute),
		nil,
	)
	if err != nil {
		t.Fatalf("BuildSignedContactCard(second) error = %v", err)
	}
	secondRaw, err := json.Marshal(secondCard)
	if err != nil {
		t.Fatalf("json.Marshal(second card) error = %v", err)
	}
	result, err := svc.ImportContactCard(ctx, secondRaw, "", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ImportContactCard(second) error = %v", err)
	}
	if !result.Updated || result.Created {
		t.Fatalf("second import should be update, got created=%v updated=%v", result.Created, result.Updated)
	}
	if result.Contact.Nickname != "robert" {
		t.Fatalf("updated nickname mismatch: got %q want %q", result.Contact.Nickname, "robert")
	}
	if result.Contact.DisplayName != "remote-alias" {
		t.Fatalf("display_name should be preserved: got %q want %q", result.Contact.DisplayName, "remote-alias")
	}
}
