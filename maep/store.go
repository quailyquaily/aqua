package maep

import (
	"context"
	"time"
)

type Store interface {
	Ensure(ctx context.Context) error
	GetIdentity(ctx context.Context) (Identity, bool, error)
	PutIdentity(ctx context.Context, identity Identity) error
	GetContactByPeerID(ctx context.Context, peerID string) (Contact, bool, error)
	GetContactByNodeUUID(ctx context.Context, nodeUUID string) (Contact, bool, error)
	PutContact(ctx context.Context, contact Contact) error
	ListContacts(ctx context.Context) ([]Contact, error)
	AppendAuditEvent(ctx context.Context, event AuditEvent) error
	ListAuditEvents(ctx context.Context, peerID string, action string, limit int) ([]AuditEvent, error)
	AppendInboxMessage(ctx context.Context, message InboxMessage) error
	ListInboxMessages(ctx context.Context, fromPeerID string, topic string, limit int) ([]InboxMessage, error)
	AppendOutboxMessage(ctx context.Context, message OutboxMessage) error
	ListOutboxMessages(ctx context.Context, toPeerID string, topic string, limit int) ([]OutboxMessage, error)
	GetDedupeRecord(ctx context.Context, fromPeerID string, topic string, idempotencyKey string) (DedupeRecord, bool, error)
	PutDedupeRecord(ctx context.Context, record DedupeRecord) error
	PruneDedupeRecords(ctx context.Context, now time.Time, maxEntries int) (int, error)
	GetProtocolHistory(ctx context.Context, peerID string) (ProtocolHistory, bool, error)
	PutProtocolHistory(ctx context.Context, history ProtocolHistory) error
}
