package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/aqua/aqua"
)

func executeCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := newRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestIDAutoInit_NoNickname(t *testing.T) {
	dir := t.TempDir()

	stdout, stderr, err := executeCLI(t, "--dir", dir, "id", "--json")
	if err != nil {
		t.Fatalf("id --json error = %v, stderr=%s", err, stderr)
	}

	var view map[string]any
	if err := json.Unmarshal([]byte(stdout), &view); err != nil {
		t.Fatalf("decode id output json error = %v, stdout=%s", err, stdout)
	}
	peerID, _ := view["peer_id"].(string)
	if strings.TrimSpace(peerID) == "" {
		t.Fatalf("peer_id should not be empty in id output")
	}

	svc := aqua.NewService(aqua.NewFileStore(dir))
	identity, ok, err := svc.GetIdentity(context.Background())
	if err != nil {
		t.Fatalf("GetIdentity() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected identity to be auto-created by `aqua id`")
	}
	if strings.TrimSpace(identity.PeerID) == "" {
		t.Fatalf("stored identity peer_id should not be empty")
	}
}

func TestIDAutoInit_WithNickname(t *testing.T) {
	dir := t.TempDir()

	stdout, stderr, err := executeCLI(t, "--dir", dir, "id", "alice", "--json")
	if err != nil {
		t.Fatalf("id alice --json error = %v, stderr=%s", err, stderr)
	}

	var view map[string]any
	if err := json.Unmarshal([]byte(stdout), &view); err != nil {
		t.Fatalf("decode id output json error = %v, stdout=%s", err, stdout)
	}
	nickname, _ := view["nickname"].(string)
	if nickname != "alice" {
		t.Fatalf("nickname mismatch: got %q want %q", nickname, "alice")
	}

	svc := aqua.NewService(aqua.NewFileStore(dir))
	identity, ok, err := svc.GetIdentity(context.Background())
	if err != nil {
		t.Fatalf("GetIdentity() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected identity to be auto-created by `aqua id alice`")
	}
	if identity.Nickname != "alice" {
		t.Fatalf("stored nickname mismatch: got %q want %q", identity.Nickname, "alice")
	}
}

func TestInboxListUnreadAutoMarksRead(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	store := aqua.NewFileStore(dir)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	receivedAt := time.Date(2026, 2, 22, 14, 0, 0, 0, time.UTC)
	if err := store.AppendInboxMessage(ctx, aqua.InboxMessage{
		MessageID:      "msg-unread-1",
		FromPeerID:     "12D3KooWpeerA",
		Topic:          "chat.message",
		ContentType:    "text/plain",
		PayloadBase64:  "aGVsbG8",
		IdempotencyKey: "k-1",
		SessionID:      "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456",
		ReceivedAt:     receivedAt,
	}); err != nil {
		t.Fatalf("AppendInboxMessage() error = %v", err)
	}

	stdout, stderr, err := executeCLI(t, "--dir", dir, "inbox", "list", "--unread", "--json")
	if err != nil {
		t.Fatalf("inbox list --unread --json error = %v, stderr=%s", err, stderr)
	}

	var records []aqua.InboxMessage
	if err := json.Unmarshal([]byte(stdout), &records); err != nil {
		t.Fatalf("decode inbox output json error = %v, stdout=%s", err, stdout)
	}
	if len(records) != 1 {
		t.Fatalf("unread list length mismatch: got %d want 1", len(records))
	}
	if records[0].MessageID != "msg-unread-1" {
		t.Fatalf("unexpected message_id: got %q", records[0].MessageID)
	}
	if records[0].Read {
		t.Fatalf("listed unread message should be shown as unread in current output")
	}

	svc := aqua.NewService(aqua.NewFileStore(dir))
	afterFirstRead, err := svc.ListInboxMessages(ctx, "", "", 10)
	if err != nil {
		t.Fatalf("ListInboxMessages() error = %v", err)
	}
	if len(afterFirstRead) != 1 {
		t.Fatalf("stored inbox length mismatch: got %d want 1", len(afterFirstRead))
	}
	if !afterFirstRead[0].Read {
		t.Fatalf("message should be auto-marked read after unread listing")
	}
	if afterFirstRead[0].ReadAt == nil {
		t.Fatalf("message read_at should be set after unread listing")
	}

	stdout2, stderr2, err := executeCLI(t, "--dir", dir, "inbox", "list", "--unread", "--json")
	if err != nil {
		t.Fatalf("second inbox list --unread --json error = %v, stderr=%s", err, stderr2)
	}
	var records2 []aqua.InboxMessage
	if err := json.Unmarshal([]byte(stdout2), &records2); err != nil {
		t.Fatalf("decode second inbox output json error = %v, stdout=%s", err, stdout2)
	}
	if len(records2) != 0 {
		t.Fatalf("second unread list should be empty, got %d", len(records2))
	}
}
