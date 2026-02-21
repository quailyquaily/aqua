package maep

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) EnsureIdentity(ctx context.Context, now time.Time) (Identity, bool, error) {
	if s == nil || s.store == nil {
		return Identity{}, false, fmt.Errorf("nil maep service")
	}
	if err := s.store.Ensure(ctx); err != nil {
		return Identity{}, false, err
	}

	identity, ok, err := s.store.GetIdentity(ctx)
	if err != nil {
		return Identity{}, false, err
	}
	if ok {
		return identity, false, nil
	}

	identity, err = GenerateIdentity(now)
	if err != nil {
		return Identity{}, false, err
	}
	if err := s.store.PutIdentity(ctx, identity); err != nil {
		return Identity{}, false, err
	}
	return identity, true, nil
}

func (s *Service) GetIdentity(ctx context.Context) (Identity, bool, error) {
	if s == nil || s.store == nil {
		return Identity{}, false, fmt.Errorf("nil maep service")
	}
	return s.store.GetIdentity(ctx)
}

func (s *Service) ExportContactCard(ctx context.Context, addresses []string, minProtocol int, maxProtocol int, now time.Time, expiresAt *time.Time) (ContactCard, []byte, error) {
	identity, ok, err := s.GetIdentity(ctx)
	if err != nil {
		return ContactCard{}, nil, err
	}
	if !ok {
		return ContactCard{}, nil, fmt.Errorf("identity is not initialized; run `aqua init`")
	}

	card, err := BuildSignedContactCard(identity, addresses, minProtocol, maxProtocol, now, expiresAt)
	if err != nil {
		return ContactCard{}, nil, err
	}

	raw, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return ContactCard{}, nil, fmt.Errorf("marshal contact card: %w", err)
	}
	raw = append(raw, '\n')
	return card, raw, nil
}

func (s *Service) ImportContactCard(ctx context.Context, rawCard []byte, displayName string, now time.Time) (ImportContactResult, error) {
	if s == nil || s.store == nil {
		return ImportContactResult{}, fmt.Errorf("nil maep service")
	}
	if err := s.store.Ensure(ctx); err != nil {
		return ImportContactResult{}, err
	}
	parsed, err := ParseAndVerifyContactCard(rawCard, now)
	if err != nil {
		return ImportContactResult{}, err
	}
	payload := parsed.Card.Payload
	peerID := strings.TrimSpace(payload.PeerID)

	existingByUUID, foundByUUID, err := s.store.GetContactByNodeUUID(ctx, payload.NodeUUID)
	if err != nil {
		return ImportContactResult{}, err
	}
	if foundByUUID && strings.TrimSpace(existingByUUID.PeerID) != peerID {
		now = normalizedNow(now)
		previousTrust := existingByUUID.TrustState
		existingByUUID.TrustState = TrustStateConflicted
		existingByUUID.UpdatedAt = now
		if err := s.store.PutContact(ctx, existingByUUID); err != nil {
			return ImportContactResult{}, err
		}
		if err := s.appendAuditEvent(ctx, now, AuditActionContactImportConflict, existingByUUID, previousTrust, existingByUUID.TrustState, "node_uuid_conflict", map[string]string{
			"existing_peer_id": existingByUUID.PeerID,
			"incoming_peer_id": peerID,
		}); err != nil {
			return ImportContactResult{}, err
		}
		return ImportContactResult{Contact: existingByUUID, Conflicted: true}, WrapProtocolError(ErrContactConflicted, "node_uuid already maps to another peer_id")
	}

	existingByPeer, foundByPeer, err := s.store.GetContactByPeerID(ctx, peerID)
	if err != nil {
		return ImportContactResult{}, err
	}
	if foundByPeer && strings.TrimSpace(existingByPeer.NodeUUID) != "" && strings.TrimSpace(existingByPeer.NodeUUID) != strings.TrimSpace(payload.NodeUUID) {
		now = normalizedNow(now)
		previousTrust := existingByPeer.TrustState
		existingByPeer.TrustState = TrustStateConflicted
		existingByPeer.UpdatedAt = now
		if err := s.store.PutContact(ctx, existingByPeer); err != nil {
			return ImportContactResult{}, err
		}
		if err := s.appendAuditEvent(ctx, now, AuditActionContactImportConflict, existingByPeer, previousTrust, existingByPeer.TrustState, "peer_id_conflict", map[string]string{
			"existing_node_uuid": existingByPeer.NodeUUID,
			"incoming_node_uuid": payload.NodeUUID,
		}); err != nil {
			return ImportContactResult{}, err
		}
		return ImportContactResult{Contact: existingByPeer, Conflicted: true}, WrapProtocolError(ErrContactConflicted, "peer_id already maps to another node_uuid")
	}

	now = normalizedNow(now)
	contact := Contact{
		NodeUUID:             payload.NodeUUID,
		PeerID:               payload.PeerID,
		NodeID:               payload.NodeID,
		DisplayName:          strings.TrimSpace(displayName),
		IdentityPubEd25519:   payload.IdentityPubEd25519,
		Addresses:            append([]string(nil), payload.Addresses...),
		MinSupportedProtocol: payload.MinSupportedProtocol,
		MaxSupportedProtocol: payload.MaxSupportedProtocol,
		IssuedAt:             payload.IssuedAt.UTC(),
		ExpiresAt:            payload.ExpiresAt,
		KeyRotationOf:        payload.KeyRotationOf,
		CardSigAlg:           parsed.Card.SigAlg,
		CardSigFormat:        parsed.Card.SigFormat,
		CardSig:              parsed.Card.Sig,
		TrustState:           TrustStateTOFU,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if contact.NodeID == "" {
		contact.NodeID = NodeIDFromPeerID(contact.PeerID)
	}

	created := true
	updated := false
	previousTrust := TrustState("")
	if foundByPeer {
		created = false
		updated = true
		contact.CreatedAt = existingByPeer.CreatedAt
		previousTrust = existingByPeer.TrustState
		contact.TrustState = preserveTrustState(existingByPeer.TrustState)
		if contact.DisplayName == "" {
			contact.DisplayName = existingByPeer.DisplayName
		}
		contact.LastSeen = existingByPeer.LastSeen
	}

	if err := s.store.PutContact(ctx, contact); err != nil {
		return ImportContactResult{}, err
	}
	action := AuditActionContactImportCreated
	if updated {
		action = AuditActionContactImportUpdated
	}
	if err := s.appendAuditEvent(ctx, now, action, contact, previousTrust, contact.TrustState, "", nil); err != nil {
		return ImportContactResult{}, err
	}

	return ImportContactResult{Contact: contact, Created: created, Updated: updated}, nil
}

func (s *Service) ListContacts(ctx context.Context) ([]Contact, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("nil maep service")
	}
	return s.store.ListContacts(ctx)
}

func (s *Service) ListInboxMessages(ctx context.Context, fromPeerID string, topic string, limit int) ([]InboxMessage, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("nil maep service")
	}
	return s.store.ListInboxMessages(ctx, fromPeerID, topic, limit)
}

func (s *Service) ListOutboxMessages(ctx context.Context, toPeerID string, topic string, limit int) ([]OutboxMessage, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("nil maep service")
	}
	return s.store.ListOutboxMessages(ctx, toPeerID, topic, limit)
}

func (s *Service) ListAuditEvents(ctx context.Context, peerID string, action string, limit int) ([]AuditEvent, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("nil maep service")
	}
	return s.store.ListAuditEvents(ctx, peerID, action, limit)
}

func (s *Service) GetContactByPeerID(ctx context.Context, peerID string) (Contact, bool, error) {
	if s == nil || s.store == nil {
		return Contact{}, false, fmt.Errorf("nil maep service")
	}
	return s.store.GetContactByPeerID(ctx, peerID)
}

func (s *Service) MarkContactVerified(ctx context.Context, peerID string, now time.Time) (Contact, error) {
	contact, ok, err := s.GetContactByPeerID(ctx, peerID)
	if err != nil {
		return Contact{}, err
	}
	if !ok {
		return Contact{}, fmt.Errorf("contact not found: %s", peerID)
	}
	if contact.TrustState == TrustStateRevoked || contact.TrustState == TrustStateConflicted {
		return Contact{}, WrapProtocolError(ErrContactConflicted, "contact cannot be promoted from state=%s", contact.TrustState)
	}
	previousTrust := contact.TrustState
	contact.TrustState = TrustStateVerified
	contact.UpdatedAt = normalizedNow(now)
	if err := s.store.PutContact(ctx, contact); err != nil {
		return Contact{}, err
	}
	if err := s.appendAuditEvent(ctx, contact.UpdatedAt, AuditActionTrustStateChanged, contact, previousTrust, contact.TrustState, "manual_verify", nil); err != nil {
		return Contact{}, err
	}
	return contact, nil
}

func (s *Service) appendAuditEvent(ctx context.Context, now time.Time, action string, contact Contact, previousTrustState TrustState, newTrustState TrustState, reason string, metadata map[string]string) error {
	event := AuditEvent{
		EventID:            "evt_" + uuid.NewString(),
		Action:             strings.TrimSpace(action),
		PeerID:             strings.TrimSpace(contact.PeerID),
		NodeUUID:           strings.TrimSpace(contact.NodeUUID),
		PreviousTrustState: previousTrustState,
		NewTrustState:      newTrustState,
		Reason:             strings.TrimSpace(reason),
		Metadata:           metadata,
		CreatedAt:          normalizedNow(now),
	}
	return s.store.AppendAuditEvent(ctx, event)
}

func preserveTrustState(existing TrustState) TrustState {
	switch existing {
	case TrustStateVerified, TrustStateConflicted, TrustStateRevoked:
		return existing
	default:
		return TrustStateTOFU
	}
}

func normalizedNow(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}
