package aqua

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/quailyquaily/aqua/internal/fsstore"
)

const (
	contactsFileVersion       = 1
	dedupeFileVersion         = 1
	inboxReadStateFileVersion = 1
	groupsFileVersion         = 1
	groupRolesFileVersion     = 1
	groupMembersFileVersion   = 1
	groupInvitesFileVersion   = 1
)

type FileStore struct {
	root string

	mu sync.Mutex
}

type contactsFile struct {
	Version  int       `json:"version"`
	Contacts []Contact `json:"contacts"`
}

type dedupeFile struct {
	Version int            `json:"version"`
	Records []DedupeRecord `json:"records"`
}

type inboxReadStateFile struct {
	Version int                  `json:"version"`
	Read    map[string]time.Time `json:"read"`
}

type groupsFile struct {
	Version int     `json:"version"`
	Groups  []Group `json:"groups"`
}

type groupRolesFile struct {
	Version int              `json:"version"`
	States  []GroupRoleState `json:"states"`
}

type groupMembersFile struct {
	Version int           `json:"version"`
	Members []GroupMember `json:"members"`
}

type groupInvitesFile struct {
	Version int           `json:"version"`
	Invites []GroupInvite `json:"invites"`
}

func NewFileStore(root string) *FileStore {
	return &FileStore{root: strings.TrimSpace(root)}
}

func (s *FileStore) Ensure(ctx context.Context) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return fsstore.EnsureDir(s.rootPath(), 0o700)
}

func (s *FileStore) GetIdentity(ctx context.Context) (Identity, bool, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return Identity{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var identity Identity
	ok, err := s.readJSONFile(s.identityPath(), &identity)
	if err != nil {
		return Identity{}, false, err
	}
	if !ok {
		return Identity{}, false, nil
	}
	return identity, true, nil
}

func (s *FileStore) PutIdentity(ctx context.Context, identity Identity) error {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withStateLock(ctx, func() error {
		return s.writeJSONFileAtomic(s.identityPath(), identity, 0o600)
	})
}

func (s *FileStore) GetContactByPeerID(ctx context.Context, peerID string) (Contact, bool, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return Contact{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	contacts, err := s.loadContactsLocked()
	if err != nil {
		return Contact{}, false, err
	}
	peerID = strings.TrimSpace(peerID)
	for _, contact := range contacts {
		if strings.TrimSpace(contact.PeerID) == peerID {
			return contact, true, nil
		}
	}
	return Contact{}, false, nil
}

func (s *FileStore) GetContactByNodeUUID(ctx context.Context, nodeUUID string) (Contact, bool, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return Contact{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	contacts, err := s.loadContactsLocked()
	if err != nil {
		return Contact{}, false, err
	}
	nodeUUID = strings.TrimSpace(nodeUUID)
	for _, contact := range contacts {
		if strings.TrimSpace(contact.NodeUUID) == nodeUUID {
			return contact, true, nil
		}
	}
	return Contact{}, false, nil
}

func (s *FileStore) PutContact(ctx context.Context, contact Contact) error {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withStateLock(ctx, func() error {
		contacts, err := s.loadContactsLocked()
		if err != nil {
			return err
		}

		replaced := false
		for i := range contacts {
			if strings.TrimSpace(contacts[i].PeerID) == strings.TrimSpace(contact.PeerID) {
				if contacts[i].CreatedAt.IsZero() {
					contacts[i].CreatedAt = contact.CreatedAt
				}
				contact.CreatedAt = contacts[i].CreatedAt
				contacts[i] = contact
				replaced = true
				break
			}
		}
		if !replaced {
			contacts = append(contacts, contact)
		}

		return s.saveContactsLocked(contacts)
	})
}

func (s *FileStore) ListContacts(ctx context.Context) ([]Contact, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	contacts, err := s.loadContactsLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Contact, len(contacts))
	copy(out, contacts)
	return out, nil
}

func (s *FileStore) DeleteContactByPeerID(ctx context.Context, peerID string) (bool, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := false
	err := s.withStateLock(ctx, func() error {
		contacts, err := s.loadContactsLocked()
		if err != nil {
			return err
		}
		peerID = strings.TrimSpace(peerID)
		if peerID == "" {
			return nil
		}

		filtered := contacts[:0]
		for _, contact := range contacts {
			if strings.TrimSpace(contact.PeerID) == peerID {
				deleted = true
				continue
			}
			filtered = append(filtered, contact)
		}
		if !deleted {
			return nil
		}
		return s.saveContactsLocked(filtered)
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}

func (s *FileStore) AppendInboxMessage(ctx context.Context, message InboxMessage) error {
	return s.AppendInboxMessages(ctx, []InboxMessage{message})
}

func (s *FileStore) AppendInboxMessages(ctx context.Context, messages []InboxMessage) error {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return err
	}
	if len(messages) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withStateLock(ctx, func() error {
		normalized := make([]InboxMessage, 0, len(messages))
		for _, message := range messages {
			message.MessageID = strings.TrimSpace(message.MessageID)
			message.FromPeerID = strings.TrimSpace(message.FromPeerID)
			message.Topic = strings.TrimSpace(message.Topic)
			message.ContentType = strings.TrimSpace(message.ContentType)
			message.PayloadBase64 = strings.TrimSpace(message.PayloadBase64)
			message.IdempotencyKey = strings.TrimSpace(message.IdempotencyKey)
			message.SessionID = strings.TrimSpace(message.SessionID)
			message.ReplyTo = strings.TrimSpace(message.ReplyTo)
			message.Read = false
			message.ReadAt = nil
			if message.ReceivedAt.IsZero() {
				message.ReceivedAt = time.Now().UTC()
			}
			normalized = append(normalized, message)
		}
		return s.appendInboxMessagesLocked(normalized)
	})
}

func (s *FileStore) ListInboxMessages(ctx context.Context, fromPeerID string, topic string, limit int) ([]InboxMessage, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.loadInboxMessagesLocked()
	if err != nil {
		return nil, err
	}
	readState, err := s.loadInboxReadStateLocked()
	if err != nil {
		return nil, err
	}
	fromPeerID = strings.TrimSpace(fromPeerID)
	topic = strings.TrimSpace(topic)

	filtered := make([]InboxMessage, 0, len(records))
	for _, record := range records {
		if fromPeerID != "" && strings.TrimSpace(record.FromPeerID) != fromPeerID {
			continue
		}
		if topic != "" && strings.TrimSpace(record.Topic) != topic {
			continue
		}
		messageID := strings.TrimSpace(record.MessageID)
		record.Read = false
		record.ReadAt = nil
		if readAt, ok := readState[messageID]; ok {
			readAtUTC := readAt.UTC()
			record.Read = true
			record.ReadAt = &readAtUTC
		}
		filtered = append(filtered, record)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].ReceivedAt.Equal(filtered[j].ReceivedAt) {
			return strings.TrimSpace(filtered[i].MessageID) > strings.TrimSpace(filtered[j].MessageID)
		}
		return filtered[i].ReceivedAt.After(filtered[j].ReceivedAt)
	})

	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	out := make([]InboxMessage, len(filtered))
	copy(out, filtered)
	return out, nil
}

func (s *FileStore) MarkInboxMessagesRead(ctx context.Context, messageIDs []string, now time.Time) (int, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return 0, err
	}
	normalizedIDs := normalizeMessageIDs(messageIDs)
	if len(normalizedIDs) == 0 {
		return 0, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	marked := 0
	err := s.withStateLock(ctx, func() error {
		records, err := s.loadInboxMessagesLocked()
		if err != nil {
			return err
		}
		existing := make(map[string]bool, len(records))
		for _, record := range records {
			id := strings.TrimSpace(record.MessageID)
			if id != "" {
				existing[id] = true
			}
		}

		readState, err := s.loadInboxReadStateLocked()
		if err != nil {
			return err
		}

		readAt := now.UTC()
		if readAt.IsZero() {
			readAt = time.Now().UTC()
		}

		changed := false
		for _, id := range normalizedIDs {
			if !existing[id] {
				continue
			}
			if _, ok := readState[id]; ok {
				continue
			}
			readState[id] = readAt
			marked++
			changed = true
		}

		if !changed {
			return nil
		}
		return s.saveInboxReadStateLocked(readState)
	})
	if err != nil {
		return 0, err
	}
	return marked, nil
}

func (s *FileStore) AppendOutboxMessage(ctx context.Context, message OutboxMessage) error {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withStateLock(ctx, func() error {
		message.MessageID = strings.TrimSpace(message.MessageID)
		message.ToPeerID = strings.TrimSpace(message.ToPeerID)
		message.Topic = strings.TrimSpace(message.Topic)
		message.ContentType = strings.TrimSpace(message.ContentType)
		message.PayloadBase64 = strings.TrimSpace(message.PayloadBase64)
		message.IdempotencyKey = strings.TrimSpace(message.IdempotencyKey)
		message.SessionID = strings.TrimSpace(message.SessionID)
		message.ReplyTo = strings.TrimSpace(message.ReplyTo)
		if message.SentAt.IsZero() {
			message.SentAt = time.Now().UTC()
		}
		return s.appendOutboxMessageLocked(message)
	})
}

func (s *FileStore) ListOutboxMessages(ctx context.Context, toPeerID string, topic string, limit int) ([]OutboxMessage, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.loadOutboxMessagesLocked()
	if err != nil {
		return nil, err
	}
	toPeerID = strings.TrimSpace(toPeerID)
	topic = strings.TrimSpace(topic)

	filtered := make([]OutboxMessage, 0, len(records))
	for _, record := range records {
		if toPeerID != "" && strings.TrimSpace(record.ToPeerID) != toPeerID {
			continue
		}
		if topic != "" && strings.TrimSpace(record.Topic) != topic {
			continue
		}
		filtered = append(filtered, record)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].SentAt.Equal(filtered[j].SentAt) {
			return strings.TrimSpace(filtered[i].MessageID) > strings.TrimSpace(filtered[j].MessageID)
		}
		return filtered[i].SentAt.After(filtered[j].SentAt)
	})

	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	out := make([]OutboxMessage, len(filtered))
	copy(out, filtered)
	return out, nil
}

func (s *FileStore) GetDedupeRecord(ctx context.Context, fromPeerID string, topic string, idempotencyKey string) (DedupeRecord, bool, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return DedupeRecord{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.loadDedupeRecordsLocked()
	if err != nil {
		return DedupeRecord{}, false, err
	}
	fromPeerID = strings.TrimSpace(fromPeerID)
	topic = strings.TrimSpace(topic)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	now := time.Now().UTC()
	for _, record := range records {
		if strings.TrimSpace(record.FromPeerID) != fromPeerID {
			continue
		}
		if strings.TrimSpace(record.Topic) != topic {
			continue
		}
		if strings.TrimSpace(record.IdempotencyKey) != idempotencyKey {
			continue
		}
		if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(now) {
			continue
		}
		return record, true, nil
	}
	return DedupeRecord{}, false, nil
}

func (s *FileStore) PutDedupeRecord(ctx context.Context, record DedupeRecord) error {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withStateLock(ctx, func() error {
		records, err := s.loadDedupeRecordsLocked()
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		record.FromPeerID = strings.TrimSpace(record.FromPeerID)
		record.Topic = strings.TrimSpace(record.Topic)
		record.IdempotencyKey = strings.TrimSpace(record.IdempotencyKey)
		if record.CreatedAt.IsZero() {
			record.CreatedAt = now
		}
		if record.ExpiresAt.IsZero() {
			record.ExpiresAt = record.CreatedAt.Add(DefaultDedupeTTL)
		}

		replaced := false
		for i := range records {
			if strings.TrimSpace(records[i].FromPeerID) != record.FromPeerID {
				continue
			}
			if strings.TrimSpace(records[i].Topic) != record.Topic {
				continue
			}
			if strings.TrimSpace(records[i].IdempotencyKey) != record.IdempotencyKey {
				continue
			}
			records[i] = record
			replaced = true
			break
		}
		if !replaced {
			records = append(records, record)
		}

		return s.saveDedupeRecordsLocked(records)
	})
}

func (s *FileStore) PruneDedupeRecords(ctx context.Context, now time.Time, maxEntries int) (int, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return 0, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if maxEntries <= 0 {
		maxEntries = DefaultDedupeMaxEntries
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	err := s.withStateLock(ctx, func() error {
		records, err := s.loadDedupeRecordsLocked()
		if err != nil {
			return err
		}
		if len(records) == 0 {
			removed = 0
			return nil
		}

		active := make([]DedupeRecord, 0, len(records))
		for _, record := range records {
			if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(now) {
				continue
			}
			active = append(active, record)
		}

		sort.Slice(active, func(i, j int) bool {
			if active[i].CreatedAt.Equal(active[j].CreatedAt) {
				leftPeer := strings.TrimSpace(active[i].FromPeerID)
				rightPeer := strings.TrimSpace(active[j].FromPeerID)
				if leftPeer != rightPeer {
					return leftPeer < rightPeer
				}
				leftTopic := strings.TrimSpace(active[i].Topic)
				rightTopic := strings.TrimSpace(active[j].Topic)
				if leftTopic != rightTopic {
					return leftTopic < rightTopic
				}
				return strings.TrimSpace(active[i].IdempotencyKey) < strings.TrimSpace(active[j].IdempotencyKey)
			}
			return active[i].CreatedAt.After(active[j].CreatedAt)
		})

		kept := active
		if len(kept) > maxEntries {
			kept = kept[:maxEntries]
		}

		removed = len(records) - len(kept)
		if removed <= 0 {
			return nil
		}
		return s.saveDedupeRecordsLocked(kept)
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

func (s *FileStore) GetGroup(ctx context.Context, groupID string) (Group, bool, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return Group{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return Group{}, false, nil
	}
	groups, err := s.loadGroupsLocked()
	if err != nil {
		return Group{}, false, err
	}
	for _, group := range groups {
		if strings.TrimSpace(group.GroupID) == groupID {
			return group, true, nil
		}
	}
	return Group{}, false, nil
}

func (s *FileStore) PutGroup(ctx context.Context, group Group) error {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withGroupLock(ctx, func() error {
		groups, err := s.loadGroupsLocked()
		if err != nil {
			return err
		}
		group.GroupID = strings.TrimSpace(group.GroupID)
		if group.GroupID == "" {
			return nil
		}
		group.ManagerPeerIDs = normalizePeerIDs(group.ManagerPeerIDs)
		group.LocalRole = GroupRole(strings.TrimSpace(string(group.LocalRole)))
		if group.MaxMembers <= 0 {
			group.MaxMembers = GroupMaxMembers
		}
		replaced := false
		for i := range groups {
			if strings.TrimSpace(groups[i].GroupID) == group.GroupID {
				if groups[i].CreatedAt.IsZero() {
					groups[i].CreatedAt = group.CreatedAt
				}
				group.CreatedAt = groups[i].CreatedAt
				groups[i] = group
				replaced = true
				break
			}
		}
		if !replaced {
			groups = append(groups, group)
		}
		return s.saveGroupsLocked(groups)
	})
}

func (s *FileStore) ListGroups(ctx context.Context) ([]Group, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	groups, err := s.loadGroupsLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Group, len(groups))
	copy(out, groups)
	return out, nil
}

func (s *FileStore) GetGroupRoleState(ctx context.Context, groupID string) (GroupRoleState, bool, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return GroupRoleState{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return GroupRoleState{}, false, nil
	}
	states, err := s.loadGroupRoleStatesLocked()
	if err != nil {
		return GroupRoleState{}, false, err
	}
	for _, state := range states {
		if strings.TrimSpace(state.GroupID) == groupID {
			return state, true, nil
		}
	}
	return GroupRoleState{}, false, nil
}

func (s *FileStore) PutGroupRoleState(ctx context.Context, state GroupRoleState) error {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withGroupLock(ctx, func() error {
		states, err := s.loadGroupRoleStatesLocked()
		if err != nil {
			return err
		}
		state.GroupID = strings.TrimSpace(state.GroupID)
		if state.GroupID == "" {
			return nil
		}
		state.Roles = normalizeGroupRoleEntries(state.Roles)
		replaced := false
		for i := range states {
			if strings.TrimSpace(states[i].GroupID) == state.GroupID {
				states[i] = state
				replaced = true
				break
			}
		}
		if !replaced {
			states = append(states, state)
		}
		return s.saveGroupRoleStatesLocked(states)
	})
}

func (s *FileStore) GetGroupMembers(ctx context.Context, groupID string) ([]GroupMember, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	groupID = strings.TrimSpace(groupID)
	members, err := s.loadGroupMembersLocked()
	if err != nil {
		return nil, err
	}
	filtered := make([]GroupMember, 0, len(members))
	for _, member := range members {
		if groupID != "" && strings.TrimSpace(member.GroupID) != groupID {
			continue
		}
		filtered = append(filtered, member)
	}
	sort.Slice(filtered, func(i, j int) bool {
		left := strings.TrimSpace(filtered[i].PeerID)
		right := strings.TrimSpace(filtered[j].PeerID)
		if left == right {
			return filtered[i].UpdatedAt.Before(filtered[j].UpdatedAt)
		}
		return left < right
	})
	return filtered, nil
}

func (s *FileStore) PutGroupMembers(ctx context.Context, groupID string, members []GroupMember) error {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withGroupLock(ctx, func() error {
		groupID = strings.TrimSpace(groupID)
		if groupID == "" {
			return nil
		}
		allMembers, err := s.loadGroupMembersLocked()
		if err != nil {
			return err
		}
		filtered := allMembers[:0]
		for _, member := range allMembers {
			if strings.TrimSpace(member.GroupID) == groupID {
				continue
			}
			filtered = append(filtered, member)
		}
		filtered = append(filtered, normalizeGroupMembers(groupID, members)...)
		return s.saveGroupMembersLocked(filtered)
	})
}

func (s *FileStore) GetGroupInvites(ctx context.Context, groupID string) ([]GroupInvite, error) {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	groupID = strings.TrimSpace(groupID)
	invites, err := s.loadGroupInvitesLocked()
	if err != nil {
		return nil, err
	}
	filtered := make([]GroupInvite, 0, len(invites))
	for _, invite := range invites {
		if groupID != "" && strings.TrimSpace(invite.GroupID) != groupID {
			continue
		}
		filtered = append(filtered, invite)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return strings.TrimSpace(filtered[i].InviteID) < strings.TrimSpace(filtered[j].InviteID)
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	return filtered, nil
}

func (s *FileStore) PutGroupInvites(ctx context.Context, groupID string, invites []GroupInvite) error {
	if err := s.ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withGroupLock(ctx, func() error {
		groupID = strings.TrimSpace(groupID)
		if groupID == "" {
			return nil
		}
		allInvites, err := s.loadGroupInvitesLocked()
		if err != nil {
			return err
		}
		filtered := allInvites[:0]
		for _, invite := range allInvites {
			if strings.TrimSpace(invite.GroupID) == groupID {
				continue
			}
			filtered = append(filtered, invite)
		}
		filtered = append(filtered, normalizeGroupInvites(groupID, invites)...)
		return s.saveGroupInvitesLocked(filtered)
	})
}

func (s *FileStore) loadContactsLocked() ([]Contact, error) {
	var file contactsFile
	ok, err := s.readJSONFile(s.contactsPath(), &file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []Contact{}, nil
	}
	out := make([]Contact, 0, len(file.Contacts))
	for _, c := range file.Contacts {
		out = append(out, c)
	}
	return out, nil
}

func (s *FileStore) loadGroupsLocked() ([]Group, error) {
	var file groupsFile
	ok, err := s.readJSONFile(s.groupsPath(), &file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []Group{}, nil
	}
	out := make([]Group, 0, len(file.Groups))
	for _, group := range file.Groups {
		group.GroupID = strings.TrimSpace(group.GroupID)
		if group.GroupID == "" {
			continue
		}
		group.ManagerPeerIDs = normalizePeerIDs(group.ManagerPeerIDs)
		group.LocalRole = GroupRole(strings.TrimSpace(string(group.LocalRole)))
		if group.MaxMembers <= 0 {
			group.MaxMembers = GroupMaxMembers
		}
		out = append(out, group)
	}
	return out, nil
}

func (s *FileStore) saveGroupsLocked(groups []Group) error {
	sort.Slice(groups, func(i, j int) bool {
		left := strings.TrimSpace(groups[i].GroupID)
		right := strings.TrimSpace(groups[j].GroupID)
		if left == right {
			return groups[i].UpdatedAt.Before(groups[j].UpdatedAt)
		}
		return left < right
	})

	file := groupsFile{
		Version: groupsFileVersion,
		Groups:  groups,
	}
	return s.writeJSONFileAtomic(s.groupsPath(), file, 0o600)
}

func (s *FileStore) loadGroupRoleStatesLocked() ([]GroupRoleState, error) {
	var file groupRolesFile
	ok, err := s.readJSONFile(s.groupRolesPath(), &file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []GroupRoleState{}, nil
	}
	out := make([]GroupRoleState, 0, len(file.States))
	for _, state := range file.States {
		state.GroupID = strings.TrimSpace(state.GroupID)
		if state.GroupID == "" {
			continue
		}
		state.Roles = normalizeGroupRoleEntries(state.Roles)
		out = append(out, state)
	}
	return out, nil
}

func (s *FileStore) saveGroupRoleStatesLocked(states []GroupRoleState) error {
	sort.Slice(states, func(i, j int) bool {
		return strings.TrimSpace(states[i].GroupID) < strings.TrimSpace(states[j].GroupID)
	})
	file := groupRolesFile{
		Version: groupRolesFileVersion,
		States:  states,
	}
	return s.writeJSONFileAtomic(s.groupRolesPath(), file, 0o600)
}

func (s *FileStore) loadGroupMembersLocked() ([]GroupMember, error) {
	var file groupMembersFile
	ok, err := s.readJSONFile(s.groupMembersPath(), &file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []GroupMember{}, nil
	}
	out := make([]GroupMember, 0, len(file.Members))
	for _, member := range file.Members {
		member.GroupID = strings.TrimSpace(member.GroupID)
		member.PeerID = strings.TrimSpace(member.PeerID)
		if member.GroupID == "" || member.PeerID == "" {
			continue
		}
		if member.LastSeenAt != nil {
			lastSeen := member.LastSeenAt.UTC()
			member.LastSeenAt = &lastSeen
		}
		out = append(out, member)
	}
	return out, nil
}

func (s *FileStore) saveGroupMembersLocked(members []GroupMember) error {
	sort.Slice(members, func(i, j int) bool {
		leftGroup := strings.TrimSpace(members[i].GroupID)
		rightGroup := strings.TrimSpace(members[j].GroupID)
		if leftGroup != rightGroup {
			return leftGroup < rightGroup
		}
		leftPeer := strings.TrimSpace(members[i].PeerID)
		rightPeer := strings.TrimSpace(members[j].PeerID)
		if leftPeer != rightPeer {
			return leftPeer < rightPeer
		}
		return members[i].UpdatedAt.Before(members[j].UpdatedAt)
	})
	file := groupMembersFile{
		Version: groupMembersFileVersion,
		Members: members,
	}
	return s.writeJSONFileAtomic(s.groupMembersPath(), file, 0o600)
}

func (s *FileStore) loadGroupInvitesLocked() ([]GroupInvite, error) {
	var file groupInvitesFile
	ok, err := s.readJSONFile(s.groupInvitesPath(), &file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []GroupInvite{}, nil
	}
	out := make([]GroupInvite, 0, len(file.Invites))
	for _, invite := range file.Invites {
		invite.GroupID = strings.TrimSpace(invite.GroupID)
		invite.InviteID = strings.TrimSpace(invite.InviteID)
		invite.InviterPeerID = strings.TrimSpace(invite.InviterPeerID)
		invite.InviteePeerID = strings.TrimSpace(invite.InviteePeerID)
		if invite.GroupID == "" || invite.InviteID == "" {
			continue
		}
		status, err := ParseGroupInviteStatus(string(invite.Status))
		if err != nil {
			continue
		}
		invite.Status = status
		if invite.RespondedAt != nil {
			at := invite.RespondedAt.UTC()
			invite.RespondedAt = &at
		}
		out = append(out, invite)
	}
	return out, nil
}

func (s *FileStore) saveGroupInvitesLocked(invites []GroupInvite) error {
	sort.Slice(invites, func(i, j int) bool {
		leftGroup := strings.TrimSpace(invites[i].GroupID)
		rightGroup := strings.TrimSpace(invites[j].GroupID)
		if leftGroup != rightGroup {
			return leftGroup < rightGroup
		}
		if invites[i].CreatedAt.Equal(invites[j].CreatedAt) {
			return strings.TrimSpace(invites[i].InviteID) < strings.TrimSpace(invites[j].InviteID)
		}
		return invites[i].CreatedAt.After(invites[j].CreatedAt)
	})
	file := groupInvitesFile{
		Version: groupInvitesFileVersion,
		Invites: invites,
	}
	return s.writeJSONFileAtomic(s.groupInvitesPath(), file, 0o600)
}

func (s *FileStore) saveContactsLocked(contacts []Contact) error {
	sort.Slice(contacts, func(i, j int) bool {
		left := strings.TrimSpace(contacts[i].PeerID)
		right := strings.TrimSpace(contacts[j].PeerID)
		if left == right {
			return contacts[i].UpdatedAt.Before(contacts[j].UpdatedAt)
		}
		return left < right
	})

	file := contactsFile{
		Version:  contactsFileVersion,
		Contacts: contacts,
	}
	return s.writeJSONFileAtomic(s.contactsPath(), file, 0o600)
}

func (s *FileStore) loadDedupeRecordsLocked() ([]DedupeRecord, error) {
	var file dedupeFile
	ok, err := s.readJSONFile(s.dedupePath(), &file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []DedupeRecord{}, nil
	}
	out := make([]DedupeRecord, 0, len(file.Records))
	for _, record := range file.Records {
		out = append(out, record)
	}
	return out, nil
}

func (s *FileStore) saveDedupeRecordsLocked(records []DedupeRecord) error {
	file := dedupeFile{Version: dedupeFileVersion, Records: records}
	return s.writeJSONFileAtomic(s.dedupePath(), file, 0o600)
}

func (s *FileStore) loadInboxReadStateLocked() (map[string]time.Time, error) {
	var file inboxReadStateFile
	ok, err := s.readJSONFile(s.inboxReadStatePath(), &file)
	if err != nil {
		return nil, err
	}
	if !ok || len(file.Read) == 0 {
		return map[string]time.Time{}, nil
	}
	out := make(map[string]time.Time, len(file.Read))
	for messageID, readAt := range file.Read {
		id := strings.TrimSpace(messageID)
		if id == "" {
			continue
		}
		if readAt.IsZero() {
			continue
		}
		out[id] = readAt.UTC()
	}
	return out, nil
}

func (s *FileStore) saveInboxReadStateLocked(read map[string]time.Time) error {
	clean := make(map[string]time.Time, len(read))
	for messageID, readAt := range read {
		id := strings.TrimSpace(messageID)
		if id == "" || readAt.IsZero() {
			continue
		}
		clean[id] = readAt.UTC()
	}
	file := inboxReadStateFile{
		Version: inboxReadStateFileVersion,
		Read:    clean,
	}
	return s.writeJSONFileAtomic(s.inboxReadStatePath(), file, 0o600)
}

func (s *FileStore) loadInboxMessagesLocked() ([]InboxMessage, error) {
	records, ok, err := s.readInboxMessagesJSONL(s.inboxPathJSONL())
	if err != nil {
		return nil, err
	}
	if !ok {
		return []InboxMessage{}, nil
	}
	return records, nil
}

func (s *FileStore) appendInboxMessageLocked(message InboxMessage) error {
	return s.appendInboxMessagesLocked([]InboxMessage{message})
}

func (s *FileStore) appendInboxMessagesLocked(messages []InboxMessage) error {
	if len(messages) == 0 {
		return nil
	}
	writer, err := fsstore.NewJSONLWriter(s.inboxPathJSONL(), fsstore.JSONLOptions{
		DirPerm:        0o700,
		FilePerm:       0o600,
		FlushEachWrite: true,
	})
	if err != nil {
		return fmt.Errorf("open inbox writer: %w", err)
	}
	defer writer.Close()
	for _, message := range messages {
		if err := writer.AppendJSON(message); err != nil {
			return fmt.Errorf("append inbox message: %w", err)
		}
	}
	return nil
}

func (s *FileStore) loadOutboxMessagesLocked() ([]OutboxMessage, error) {
	records, ok, err := s.readOutboxMessagesJSONL(s.outboxPathJSONL())
	if err != nil {
		return nil, err
	}
	if !ok {
		return []OutboxMessage{}, nil
	}
	return records, nil
}

func (s *FileStore) appendOutboxMessageLocked(message OutboxMessage) error {
	writer, err := fsstore.NewJSONLWriter(s.outboxPathJSONL(), fsstore.JSONLOptions{
		DirPerm:        0o700,
		FilePerm:       0o600,
		FlushEachWrite: true,
	})
	if err != nil {
		return fmt.Errorf("open outbox writer: %w", err)
	}
	defer writer.Close()
	if err := writer.AppendJSON(message); err != nil {
		return fmt.Errorf("append outbox message: %w", err)
	}
	return nil
}

func (s *FileStore) readJSONFile(path string, out any) (bool, error) {
	ok, err := fsstore.ReadJSON(path, out)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	return ok, nil
}

func (s *FileStore) writeJSONFileAtomic(path string, v any, perm os.FileMode) error {
	return fsstore.WriteJSONAtomic(path, v, fsstore.FileOptions{
		DirPerm:  0o700,
		FilePerm: perm,
	})
}

func (s *FileStore) readInboxMessagesJSONL(path string) ([]InboxMessage, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("open inbox jsonl %s: %w", path, err)
	}
	defer file.Close()

	records := make([]InboxMessage, 0, 64)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var message InboxMessage
		if err := json.Unmarshal(line, &message); err != nil {
			return nil, false, fmt.Errorf("decode inbox jsonl %s: %w", path, err)
		}
		message.SessionID = strings.TrimSpace(message.SessionID)
		message.ReplyTo = strings.TrimSpace(message.ReplyTo)
		message.Read = false
		message.ReadAt = nil
		records = append(records, message)
	}
	if err := scanner.Err(); err != nil {
		return nil, false, fmt.Errorf("scan inbox jsonl %s: %w", path, err)
	}
	return records, true, nil
}

func (s *FileStore) readOutboxMessagesJSONL(path string) ([]OutboxMessage, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("open outbox jsonl %s: %w", path, err)
	}
	defer file.Close()

	records := make([]OutboxMessage, 0, 64)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var message OutboxMessage
		if err := json.Unmarshal(line, &message); err != nil {
			return nil, false, fmt.Errorf("decode outbox jsonl %s: %w", path, err)
		}
		message.SessionID = strings.TrimSpace(message.SessionID)
		message.ReplyTo = strings.TrimSpace(message.ReplyTo)
		records = append(records, message)
	}
	if err := scanner.Err(); err != nil {
		return nil, false, fmt.Errorf("scan outbox jsonl %s: %w", path, err)
	}
	return records, true, nil
}

func (s *FileStore) ensureNotCanceled(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (s *FileStore) rootPath() string {
	root := strings.TrimSpace(s.root)
	if root == "" {
		return "aqua"
	}
	return filepath.Clean(root)
}

func (s *FileStore) lockRootPath() string {
	return filepath.Join(s.rootPath(), ".fslocks")
}

func (s *FileStore) withStateLock(ctx context.Context, fn func() error) error {
	return s.withLock(ctx, "state.main", fn)
}

func (s *FileStore) withGroupLock(ctx context.Context, fn func() error) error {
	return s.withLock(ctx, "state.group", fn)
}

func (s *FileStore) withLock(ctx context.Context, key string, fn func() error) error {
	lockPath, err := fsstore.BuildLockPath(s.lockRootPath(), key)
	if err != nil {
		return err
	}
	return fsstore.WithLock(ctx, lockPath, fn)
}

func (s *FileStore) identityPath() string {
	return filepath.Join(s.rootPath(), "identity.json")
}

func (s *FileStore) contactsPath() string {
	return filepath.Join(s.rootPath(), "contacts.json")
}

func (s *FileStore) dedupePath() string {
	return filepath.Join(s.rootPath(), "dedupe_records.json")
}

func (s *FileStore) inboxReadStatePath() string {
	return filepath.Join(s.rootPath(), "inbox_read_state.json")
}

func (s *FileStore) inboxPathJSONL() string {
	return filepath.Join(s.rootPath(), "inbox_messages.jsonl")
}

func (s *FileStore) outboxPathJSONL() string {
	return filepath.Join(s.rootPath(), "outbox_messages.jsonl")
}

func (s *FileStore) groupsPath() string {
	return filepath.Join(s.rootPath(), "groups.json")
}

func (s *FileStore) groupRolesPath() string {
	return filepath.Join(s.rootPath(), "group_roles.json")
}

func (s *FileStore) groupMembersPath() string {
	return filepath.Join(s.rootPath(), "group_members.json")
}

func (s *FileStore) groupInvitesPath() string {
	return filepath.Join(s.rootPath(), "group_invites.json")
}

func normalizeMessageIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func normalizePeerIDs(peerIDs []string) []string {
	if len(peerIDs) == 0 {
		return nil
	}
	out := make([]string, 0, len(peerIDs))
	seen := map[string]bool{}
	for _, raw := range peerIDs {
		peerID := strings.TrimSpace(raw)
		if peerID == "" || seen[peerID] {
			continue
		}
		seen[peerID] = true
		out = append(out, peerID)
	}
	sort.Strings(out)
	return out
}

func normalizeGroupRoleEntries(roles []GroupRoleEntry) []GroupRoleEntry {
	if len(roles) == 0 {
		return nil
	}
	out := make([]GroupRoleEntry, 0, len(roles))
	seen := map[string]bool{}
	for _, role := range roles {
		role.PeerID = strings.TrimSpace(role.PeerID)
		if role.PeerID == "" || seen[role.PeerID] {
			continue
		}
		parsedRole, err := ParseGroupRole(string(role.Role))
		if err != nil {
			continue
		}
		role.Role = parsedRole
		role.UpdatedAt = role.UpdatedAt.UTC()
		if role.UpdatedAt.IsZero() {
			role.UpdatedAt = time.Now().UTC()
		}
		seen[role.PeerID] = true
		out = append(out, role)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.TrimSpace(out[i].PeerID) < strings.TrimSpace(out[j].PeerID)
	})
	return out
}

func normalizeGroupMembers(groupID string, members []GroupMember) []GroupMember {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" || len(members) == 0 {
		return nil
	}
	out := make([]GroupMember, 0, len(members))
	seen := map[string]bool{}
	for _, member := range members {
		member.GroupID = groupID
		member.PeerID = strings.TrimSpace(member.PeerID)
		if member.PeerID == "" || seen[member.PeerID] {
			continue
		}
		if member.JoinedAt.IsZero() {
			member.JoinedAt = time.Now().UTC()
		}
		member.JoinedAt = member.JoinedAt.UTC()
		if member.LastSeenAt != nil {
			lastSeen := member.LastSeenAt.UTC()
			member.LastSeenAt = &lastSeen
		}
		member.UpdatedAt = member.UpdatedAt.UTC()
		if member.UpdatedAt.IsZero() {
			member.UpdatedAt = time.Now().UTC()
		}
		seen[member.PeerID] = true
		out = append(out, member)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.TrimSpace(out[i].PeerID) < strings.TrimSpace(out[j].PeerID)
	})
	return out
}

func normalizeGroupInvites(groupID string, invites []GroupInvite) []GroupInvite {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" || len(invites) == 0 {
		return nil
	}
	out := make([]GroupInvite, 0, len(invites))
	seen := map[string]bool{}
	for _, invite := range invites {
		invite.GroupID = groupID
		invite.InviteID = strings.TrimSpace(invite.InviteID)
		invite.InviterPeerID = strings.TrimSpace(invite.InviterPeerID)
		invite.InviteePeerID = strings.TrimSpace(invite.InviteePeerID)
		if invite.InviteID == "" || seen[invite.InviteID] {
			continue
		}
		status, err := ParseGroupInviteStatus(string(invite.Status))
		if err != nil {
			continue
		}
		invite.Status = status
		invite.CreatedAt = invite.CreatedAt.UTC()
		if invite.CreatedAt.IsZero() {
			invite.CreatedAt = time.Now().UTC()
		}
		invite.ExpiresAt = invite.ExpiresAt.UTC()
		if invite.ExpiresAt.IsZero() {
			invite.ExpiresAt = invite.CreatedAt.Add(GroupInviteTTL)
		}
		if invite.RespondedAt != nil {
			at := invite.RespondedAt.UTC()
			invite.RespondedAt = &at
		}
		seen[invite.InviteID] = true
		out = append(out, invite)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return strings.TrimSpace(out[i].InviteID) < strings.TrimSpace(out[j].InviteID)
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}
