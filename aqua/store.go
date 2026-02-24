package aqua

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
	DeleteContactByPeerID(ctx context.Context, peerID string) (bool, error)
	ListContacts(ctx context.Context) ([]Contact, error)
	AppendInboxMessage(ctx context.Context, message InboxMessage) error
	AppendInboxMessages(ctx context.Context, messages []InboxMessage) error
	ListInboxMessages(ctx context.Context, fromPeerID string, topic string, limit int) ([]InboxMessage, error)
	MarkInboxMessagesRead(ctx context.Context, messageIDs []string, now time.Time) (int, error)
	AppendOutboxMessage(ctx context.Context, message OutboxMessage) error
	ListOutboxMessages(ctx context.Context, toPeerID string, topic string, limit int) ([]OutboxMessage, error)
	GetDedupeRecord(ctx context.Context, fromPeerID string, topic string, idempotencyKey string) (DedupeRecord, bool, error)
	PutDedupeRecord(ctx context.Context, record DedupeRecord) error
	PruneDedupeRecords(ctx context.Context, now time.Time, maxEntries int) (int, error)
	GetGroup(ctx context.Context, groupID string) (Group, bool, error)
	PutGroup(ctx context.Context, group Group) error
	ListGroups(ctx context.Context) ([]Group, error)
	GetGroupRoleState(ctx context.Context, groupID string) (GroupRoleState, bool, error)
	PutGroupRoleState(ctx context.Context, state GroupRoleState) error
	GetGroupMembers(ctx context.Context, groupID string) ([]GroupMember, error)
	PutGroupMembers(ctx context.Context, groupID string, members []GroupMember) error
	GetGroupInvites(ctx context.Context, groupID string) ([]GroupInvite, error)
	PutGroupInvites(ctx context.Context, groupID string, invites []GroupInvite) error
}
