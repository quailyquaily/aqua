package aqua

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreIdentityAndContacts(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "aqua")
	store := NewFileStore(root)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)
	identity := Identity{
		NodeUUID:            "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456",
		PeerID:              "12D3KooWexample",
		NodeID:              "aqua:12D3KooWexample",
		IdentityPubEd25519:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		IdentityPrivEd25519: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := store.PutIdentity(ctx, identity); err != nil {
		t.Fatalf("PutIdentity() error = %v", err)
	}

	gotIdentity, ok, err := store.GetIdentity(ctx)
	if err != nil {
		t.Fatalf("GetIdentity() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetIdentity() expected ok=true")
	}
	if gotIdentity.PeerID != identity.PeerID {
		t.Fatalf("identity peer_id mismatch: got %s want %s", gotIdentity.PeerID, identity.PeerID)
	}

	contact := Contact{
		NodeUUID:             "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f789",
		PeerID:               "12D3KooWcontact",
		NodeID:               "aqua:12D3KooWcontact",
		IdentityPubEd25519:   "ccccccccccccccccccccccccccccccccccccccccccc",
		Addresses:            []string{"/dns4/example.com/udp/6371/quic-v1/p2p/12D3KooWcontact"},
		MinSupportedProtocol: 1,
		MaxSupportedProtocol: 1,
		IssuedAt:             now,
		CardSigAlg:           ContactCardSigAlgEd25519,
		CardSigFormat:        ContactCardSigFormatJCS,
		CardSig:              "sig",
		TrustState:           TrustStateTOFU,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := store.PutContact(ctx, contact); err != nil {
		t.Fatalf("PutContact() error = %v", err)
	}

	gotContact, ok, err := store.GetContactByPeerID(ctx, contact.PeerID)
	if err != nil {
		t.Fatalf("GetContactByPeerID() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetContactByPeerID() expected ok=true")
	}
	if gotContact.NodeUUID != contact.NodeUUID {
		t.Fatalf("contact node_uuid mismatch: got %s want %s", gotContact.NodeUUID, contact.NodeUUID)
	}

	deleted, err := store.DeleteContactByPeerID(ctx, contact.PeerID)
	if err != nil {
		t.Fatalf("DeleteContactByPeerID() error = %v", err)
	}
	if !deleted {
		t.Fatalf("DeleteContactByPeerID() expected deleted=true")
	}
	_, ok, err = store.GetContactByPeerID(ctx, contact.PeerID)
	if err != nil {
		t.Fatalf("GetContactByPeerID(after delete) error = %v", err)
	}
	if ok {
		t.Fatalf("GetContactByPeerID(after delete) expected ok=false")
	}

	deleted, err = store.DeleteContactByPeerID(ctx, contact.PeerID)
	if err != nil {
		t.Fatalf("DeleteContactByPeerID(second call) error = %v", err)
	}
	if deleted {
		t.Fatalf("DeleteContactByPeerID(second call) expected deleted=false")
	}
}

func TestFileStoreDedupeAndMessages(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "aqua")
	store := NewFileStore(root)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)
	record := DedupeRecord{
		FromPeerID:     "12D3KooWpeerA",
		Topic:          "chat.message",
		IdempotencyKey: "m-001",
		CreatedAt:      now,
		// Keep expiration relative to wall clock because GetDedupeRecord uses time.Now().
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	if err := store.PutDedupeRecord(ctx, record); err != nil {
		t.Fatalf("PutDedupeRecord() error = %v", err)
	}
	gotRecord, ok, err := store.GetDedupeRecord(ctx, record.FromPeerID, record.Topic, record.IdempotencyKey)
	if err != nil {
		t.Fatalf("GetDedupeRecord() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetDedupeRecord() expected ok=true")
	}
	if gotRecord.IdempotencyKey != record.IdempotencyKey {
		t.Fatalf("dedupe idempotency_key mismatch: got %s want %s", gotRecord.IdempotencyKey, record.IdempotencyKey)
	}

	messageA := InboxMessage{
		MessageID:      "msg-001",
		FromPeerID:     "12D3KooWpeerA",
		Topic:          "chat.message",
		ContentType:    "application/json",
		PayloadBase64:  "eyJ0ZXh0IjoiaGV5In0",
		IdempotencyKey: "m-001",
		SessionID:      "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456",
		ReceivedAt:     now,
	}
	if err := store.AppendInboxMessage(ctx, messageA); err != nil {
		t.Fatalf("AppendInboxMessage() error = %v", err)
	}
	messageB := InboxMessage{
		MessageID:      "msg-002",
		FromPeerID:     "12D3KooWpeerB",
		Topic:          "chat.message",
		ContentType:    "text/plain",
		PayloadBase64:  "aGVsbG8",
		IdempotencyKey: "m-101",
		SessionID:      "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f457",
		ReceivedAt:     now.Add(5 * time.Second),
	}
	if err := store.AppendInboxMessage(ctx, messageB); err != nil {
		t.Fatalf("AppendInboxMessage() second error = %v", err)
	}

	inbox, err := store.ListInboxMessages(ctx, "12D3KooWpeerA", "", 10)
	if err != nil {
		t.Fatalf("ListInboxMessages() error = %v", err)
	}
	if len(inbox) != 1 {
		t.Fatalf("ListInboxMessages() length mismatch: got %d want 1", len(inbox))
	}
	if inbox[0].MessageID != messageA.MessageID {
		t.Fatalf("inbox message_id mismatch: got %s want %s", inbox[0].MessageID, messageA.MessageID)
	}
	if inbox[0].SessionID != messageA.SessionID {
		t.Fatalf("inbox session_id mismatch: got %s want %s", inbox[0].SessionID, messageA.SessionID)
	}
	if inbox[0].Read {
		t.Fatalf("expected unread inbox message before mark-read")
	}
	if inbox[0].ReadAt != nil {
		t.Fatalf("expected nil read_at before mark-read")
	}

	marked, err := store.MarkInboxMessagesRead(ctx, []string{messageA.MessageID}, now.Add(20*time.Second))
	if err != nil {
		t.Fatalf("MarkInboxMessagesRead() error = %v", err)
	}
	if marked != 1 {
		t.Fatalf("MarkInboxMessagesRead() marked mismatch: got %d want 1", marked)
	}
	inboxAfterRead, err := store.ListInboxMessages(ctx, "12D3KooWpeerA", "", 10)
	if err != nil {
		t.Fatalf("ListInboxMessages(after mark-read) error = %v", err)
	}
	if len(inboxAfterRead) != 1 {
		t.Fatalf("ListInboxMessages(after mark-read) length mismatch: got %d want 1", len(inboxAfterRead))
	}
	if !inboxAfterRead[0].Read {
		t.Fatalf("expected inbox message to be read after mark-read")
	}
	if inboxAfterRead[0].ReadAt == nil {
		t.Fatalf("expected read_at after mark-read")
	}

	inboxUnread, err := store.ListInboxMessages(ctx, "12D3KooWpeerB", "", 10)
	if err != nil {
		t.Fatalf("ListInboxMessages(peerB) error = %v", err)
	}
	if len(inboxUnread) != 1 {
		t.Fatalf("ListInboxMessages(peerB) length mismatch: got %d want 1", len(inboxUnread))
	}
	if inboxUnread[0].Read {
		t.Fatalf("expected unrelated inbox message to remain unread")
	}

	outboxA := OutboxMessage{
		MessageID:      "msg-901",
		ToPeerID:       "12D3KooWpeerB",
		Topic:          "dm.reply.v1",
		ContentType:    "application/json",
		PayloadBase64:  "eyJ0ZXh0IjoicG9uZyJ9",
		IdempotencyKey: "r-001",
		SessionID:      "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f458",
		SentAt:         now.Add(15 * time.Second),
	}
	if err := store.AppendOutboxMessage(ctx, outboxA); err != nil {
		t.Fatalf("AppendOutboxMessage() error = %v", err)
	}
	outbox, err := store.ListOutboxMessages(ctx, "12D3KooWpeerB", "", 10)
	if err != nil {
		t.Fatalf("ListOutboxMessages() error = %v", err)
	}
	if len(outbox) != 1 {
		t.Fatalf("ListOutboxMessages() length mismatch: got %d want 1", len(outbox))
	}
	if outbox[0].MessageID != outboxA.MessageID {
		t.Fatalf("outbox message_id mismatch: got %s want %s", outbox[0].MessageID, outboxA.MessageID)
	}

}

func TestFileStorePruneDedupeRecords_GlobalMaxEntriesAndTTL(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "aqua")
	store := NewFileStore(root)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	now := time.Date(2026, 2, 7, 4, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		record := DedupeRecord{
			FromPeerID:     "12D3KooWpeerA",
			Topic:          "chat.message",
			IdempotencyKey: "m-" + time.Date(2026, 2, 7, 4, 0, i, 0, time.UTC).Format("150405"),
			CreatedAt:      now.Add(time.Duration(i) * time.Minute),
			ExpiresAt:      now.Add(24 * time.Hour),
		}
		if err := store.PutDedupeRecord(ctx, record); err != nil {
			t.Fatalf("PutDedupeRecord() error = %v", err)
		}
	}
	expired := DedupeRecord{
		FromPeerID:     "12D3KooWpeerA",
		Topic:          "chat.message",
		IdempotencyKey: "m-expired",
		CreatedAt:      now.Add(-48 * time.Hour),
		ExpiresAt:      now.Add(-24 * time.Hour),
	}
	if err := store.PutDedupeRecord(ctx, expired); err != nil {
		t.Fatalf("PutDedupeRecord(expired) error = %v", err)
	}

	removed, err := store.PruneDedupeRecords(ctx, now, 3)
	if err != nil {
		t.Fatalf("PruneDedupeRecords() error = %v", err)
	}
	if removed != 3 {
		t.Fatalf("PruneDedupeRecords() removed mismatch: got %d want 3", removed)
	}

	records, err := store.loadDedupeRecordsLocked()
	if err != nil {
		t.Fatalf("loadDedupeRecordsLocked() error = %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("remaining dedupe records mismatch: got %d want 3", len(records))
	}
}

func TestFileStoreAppendInboxMessage_DoesNotAutoFillSessionID(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "aqua")
	store := NewFileStore(root)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	msg := InboxMessage{
		MessageID:      "msg-raw-1",
		FromPeerID:     "12D3KooWpeerZ",
		Topic:          "custom.note.v1",
		ContentType:    "text/plain",
		PayloadBase64:  "aGVsbG8",
		IdempotencyKey: "id-1",
	}
	if err := store.AppendInboxMessage(ctx, msg); err != nil {
		t.Fatalf("AppendInboxMessage() error = %v", err)
	}

	records, err := store.ListInboxMessages(ctx, msg.FromPeerID, msg.Topic, 10)
	if err != nil {
		t.Fatalf("ListInboxMessages() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("ListInboxMessages() len mismatch: got %d want 1", len(records))
	}
	if records[0].SessionID != "" {
		t.Fatalf("expected empty session_id, got %q", records[0].SessionID)
	}
}
