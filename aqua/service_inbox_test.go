package aqua

import (
	"context"
	"testing"
	"time"
)

func TestServiceMarkInboxMessagesRead(t *testing.T) {
	ctx := context.Background()
	store := NewFileStore(t.TempDir())
	svc := NewService(store)

	now := time.Date(2026, 2, 22, 13, 0, 0, 0, time.UTC)
	if err := store.AppendInboxMessage(ctx, InboxMessage{
		MessageID:      "msg-001",
		FromPeerID:     "12D3KooWpeerA",
		Topic:          "chat.message",
		ContentType:    "text/plain",
		PayloadBase64:  "aGVsbG8",
		IdempotencyKey: "k-001",
		SessionID:      "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456",
		ReceivedAt:     now,
	}); err != nil {
		t.Fatalf("AppendInboxMessage() error = %v", err)
	}

	marked, err := svc.MarkInboxMessagesRead(ctx, []string{"msg-001"}, now.Add(time.Second))
	if err != nil {
		t.Fatalf("MarkInboxMessagesRead() error = %v", err)
	}
	if marked != 1 {
		t.Fatalf("MarkInboxMessagesRead() marked mismatch: got %d want 1", marked)
	}

	records, err := svc.ListInboxMessages(ctx, "12D3KooWpeerA", "", 10)
	if err != nil {
		t.Fatalf("ListInboxMessages() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("ListInboxMessages() length mismatch: got %d want 1", len(records))
	}
	if !records[0].Read {
		t.Fatalf("expected read=true after mark-read")
	}
	if records[0].ReadAt == nil {
		t.Fatalf("expected read_at after mark-read")
	}
}
