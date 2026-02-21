package aqua

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) EnsureIdentity(ctx context.Context, now time.Time) (Identity, bool, error) {
	if s == nil || s.store == nil {
		return Identity{}, false, fmt.Errorf("nil aqua service")
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
		return Identity{}, false, fmt.Errorf("nil aqua service")
	}
	return s.store.GetIdentity(ctx)
}

func (s *Service) SetIdentityNickname(ctx context.Context, nickname string, now time.Time) (Identity, error) {
	if s == nil || s.store == nil {
		return Identity{}, fmt.Errorf("nil aqua service")
	}

	identity, ok, err := s.store.GetIdentity(ctx)
	if err != nil {
		return Identity{}, err
	}
	if !ok {
		return Identity{}, fmt.Errorf("identity not found; run `aqua init`")
	}

	trimmedNickname := strings.TrimSpace(nickname)
	if identity.Nickname == trimmedNickname {
		return identity, nil
	}

	identity.Nickname = trimmedNickname
	identity.UpdatedAt = normalizedNow(now)
	if err := s.store.PutIdentity(ctx, identity); err != nil {
		return Identity{}, err
	}
	return identity, nil
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
		return ImportContactResult{}, fmt.Errorf("nil aqua service")
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
		existingByUUID.TrustState = TrustStateConflicted
		existingByUUID.UpdatedAt = now
		if err := s.store.PutContact(ctx, existingByUUID); err != nil {
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
		existingByPeer.TrustState = TrustStateConflicted
		existingByPeer.UpdatedAt = now
		if err := s.store.PutContact(ctx, existingByPeer); err != nil {
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
		Nickname:             strings.TrimSpace(payload.Nickname),
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
	if foundByPeer {
		created = false
		updated = true
		contact.CreatedAt = existingByPeer.CreatedAt
		contact.TrustState = preserveTrustState(existingByPeer.TrustState)
		if contact.DisplayName == "" {
			contact.DisplayName = existingByPeer.DisplayName
		}
		contact.LastSeen = existingByPeer.LastSeen
	}

	if err := s.store.PutContact(ctx, contact); err != nil {
		return ImportContactResult{}, err
	}

	return ImportContactResult{Contact: contact, Created: created, Updated: updated}, nil
}

func (s *Service) ListContacts(ctx context.Context) ([]Contact, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("nil aqua service")
	}
	return s.store.ListContacts(ctx)
}

func (s *Service) ListInboxMessages(ctx context.Context, fromPeerID string, topic string, limit int) ([]InboxMessage, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("nil aqua service")
	}
	return s.store.ListInboxMessages(ctx, fromPeerID, topic, limit)
}

func (s *Service) ListOutboxMessages(ctx context.Context, toPeerID string, topic string, limit int) ([]OutboxMessage, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("nil aqua service")
	}
	return s.store.ListOutboxMessages(ctx, toPeerID, topic, limit)
}

func (s *Service) GetContactByPeerID(ctx context.Context, peerID string) (Contact, bool, error) {
	if s == nil || s.store == nil {
		return Contact{}, false, fmt.Errorf("nil aqua service")
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
	contact.TrustState = TrustStateVerified
	contact.UpdatedAt = normalizedNow(now)
	if err := s.store.PutContact(ctx, contact); err != nil {
		return Contact{}, err
	}
	return contact, nil
}

func (s *Service) DeleteContact(ctx context.Context, peerID string, now time.Time) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("nil aqua service")
	}
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return fmt.Errorf("peer_id is required")
	}
	deleted, err := s.store.DeleteContactByPeerID(ctx, peerID)
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("contact not found: %s", peerID)
	}
	return nil
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
