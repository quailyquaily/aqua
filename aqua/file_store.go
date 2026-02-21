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
	if err := s.ensureNotCanceled(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withStateLock(ctx, func() error {
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
		return s.appendInboxMessageLocked(message)
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
	writer, err := fsstore.NewJSONLWriter(s.inboxPathJSONL(), fsstore.JSONLOptions{
		DirPerm:        0o700,
		FilePerm:       0o600,
		FlushEachWrite: true,
	})
	if err != nil {
		return fmt.Errorf("open inbox writer: %w", err)
	}
	defer writer.Close()
	if err := writer.AppendJSON(message); err != nil {
		return fmt.Errorf("append inbox message: %w", err)
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
