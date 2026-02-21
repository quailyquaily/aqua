package maep

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreIdentityAndContacts(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "maep")
	store := NewFileStore(root)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)
	identity := Identity{
		NodeUUID:            "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456",
		PeerID:              "12D3KooWexample",
		NodeID:              "maep:12D3KooWexample",
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
		NodeID:               "maep:12D3KooWcontact",
		IdentityPubEd25519:   "ccccccccccccccccccccccccccccccccccccccccccc",
		Addresses:            []string{"/dns4/example.com/udp/4001/quic-v1/p2p/12D3KooWcontact"},
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
}

func TestFileStoreDedupeAndProtocolHistory(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "maep")
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

	history := ProtocolHistory{
		PeerID:                 "12D3KooWpeerB",
		LastRemoteMaxProtocol:  1,
		LastNegotiatedProtocol: 1,
		UpdatedAt:              now,
	}
	if err := store.PutProtocolHistory(ctx, history); err != nil {
		t.Fatalf("PutProtocolHistory() error = %v", err)
	}
	gotHistory, ok, err := store.GetProtocolHistory(ctx, history.PeerID)
	if err != nil {
		t.Fatalf("GetProtocolHistory() error = %v", err)
	}
	if !ok {
		t.Fatalf("GetProtocolHistory() expected ok=true")
	}
	if gotHistory.LastNegotiatedProtocol != history.LastNegotiatedProtocol {
		t.Fatalf("protocol history negotiated mismatch: got %d want %d", gotHistory.LastNegotiatedProtocol, history.LastNegotiatedProtocol)
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

	audit := AuditEvent{
		EventID:            "evt-001",
		Action:             AuditActionTrustStateChanged,
		PeerID:             "12D3KooWpeerA",
		NodeUUID:           "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f789",
		PreviousTrustState: TrustStateTOFU,
		NewTrustState:      TrustStateVerified,
		Reason:             "manual_verify",
		CreatedAt:          now.Add(10 * time.Second),
	}
	if err := store.AppendAuditEvent(ctx, audit); err != nil {
		t.Fatalf("AppendAuditEvent() error = %v", err)
	}
	audits, err := store.ListAuditEvents(ctx, "12D3KooWpeerA", AuditActionTrustStateChanged, 10)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if len(audits) != 1 {
		t.Fatalf("ListAuditEvents() length mismatch: got %d want 1", len(audits))
	}
	if audits[0].EventID != audit.EventID {
		t.Fatalf("audit event_id mismatch: got %s want %s", audits[0].EventID, audit.EventID)
	}
}

func TestFileStorePruneDedupeRecords_GlobalMaxEntriesAndTTL(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "maep")
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

func TestSessionScopeByTopic_DialogueTopicsShareScope(t *testing.T) {
	share := SessionScopeByTopic("share.proactive.v1")
	reply := SessionScopeByTopic("dm.reply.v1")
	checkin := SessionScopeByTopic("dm.checkin.v1")
	chat := SessionScopeByTopic("chat.message")

	want := "dialogue.v1"
	for _, got := range []string{share, reply, checkin, chat} {
		if got != want {
			t.Fatalf("SessionScopeByTopic mismatch: got %q want %q", got, want)
		}
	}

	other := SessionScopeByTopic("agent.status.v1")
	if other != "topic:agent.status.v1" {
		t.Fatalf("SessionScopeByTopic for non-dialogue topic mismatch: got %q", other)
	}
}

func TestFileStoreAppendInboxMessage_DoesNotAutoFillSessionID(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "maep")
	store := NewFileStore(root)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	msg := InboxMessage{
		MessageID:      "msg-raw-1",
		FromPeerID:     "12D3KooWpeerZ",
		Topic:          "agent.status.v1",
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
