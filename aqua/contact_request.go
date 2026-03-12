package aqua

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const ContactControlVersion = 1

type ContactControlAction string

const (
	ContactControlActionRequest ContactControlAction = "contact.request"
	ContactControlActionAccept  ContactControlAction = "contact.request.accept"
	ContactControlActionReject  ContactControlAction = "contact.request.reject"
)

type ContactRequestStatus string

const (
	ContactRequestStatusPending  ContactRequestStatus = "pending"
	ContactRequestStatusAccepted ContactRequestStatus = "accepted"
	ContactRequestStatusRejected ContactRequestStatus = "rejected"
)

type ContactRequest struct {
	RequestID         string               `json:"request_id"`
	InitiatorPeerID   string               `json:"initiator_peer_id"`
	RecipientPeerID   string               `json:"recipient_peer_id"`
	InitiatorCardJSON string               `json:"initiator_card_json,omitempty"`
	DisplayName       string               `json:"display_name,omitempty"`
	VerifyOnAccept    bool                 `json:"verify_on_accept"`
	Status            ContactRequestStatus `json:"status"`
	CreatedAt         time.Time            `json:"created_at"`
	RespondedAt       *time.Time           `json:"responded_at,omitempty"`
}

type ContactRequestControlMessage struct {
	Version        int                  `json:"version"`
	Action         ContactControlAction `json:"action"`
	RequestID      string               `json:"request_id"`
	FromPeerID     string               `json:"from_peer_id"`
	ToPeerID       string               `json:"to_peer_id"`
	VerifyOnAccept bool                 `json:"verify_on_accept"`
	CreatedAt      time.Time            `json:"created_at"`
	ContactCard    json.RawMessage      `json:"contact_card"`
}

type ContactDecisionControlMessage struct {
	Version        int                  `json:"version"`
	Action         ContactControlAction `json:"action"`
	RequestID      string               `json:"request_id"`
	FromPeerID     string               `json:"from_peer_id"`
	ToPeerID       string               `json:"to_peer_id"`
	VerifyOnAccept bool                 `json:"verify_on_accept"`
	RespondedAt    time.Time            `json:"responded_at"`
	ContactCard    json.RawMessage      `json:"contact_card,omitempty"`
}

type contactControlHeader struct {
	Version int                  `json:"version"`
	Action  ContactControlAction `json:"action"`
}

func ParseContactRequestStatus(raw string) (ContactRequestStatus, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ContactRequestStatusPending):
		return ContactRequestStatusPending, nil
	case string(ContactRequestStatusAccepted):
		return ContactRequestStatusAccepted, nil
	case string(ContactRequestStatusRejected):
		return ContactRequestStatusRejected, nil
	default:
		return "", fmt.Errorf("invalid contact request status %q", raw)
	}
}

func DecodeContactControlEvent(fromPeerID string, raw []byte, receivedAt time.Time) (ContactControlEvent, error) {
	var header contactControlHeader
	if err := decodeRPCJSON(raw, &header); err != nil {
		return ContactControlEvent{}, WrapProtocolError(ErrInvalidParams, "contact control decode failed: %v", err)
	}
	receivedAt = normalizedNow(receivedAt)
	switch header.Action {
	case ContactControlActionRequest:
		var msg ContactRequestControlMessage
		if err := decodeRPCJSON(raw, &msg); err != nil {
			return ContactControlEvent{}, WrapProtocolError(ErrInvalidParams, "contact request decode failed: %v", err)
		}
		createdAt := normalizedNow(msg.CreatedAt)
		return ContactControlEvent{
			FromPeerID:     strings.TrimSpace(fromPeerID),
			ToPeerID:       strings.TrimSpace(msg.ToPeerID),
			Action:         msg.Action,
			RequestID:      strings.TrimSpace(msg.RequestID),
			VerifyOnAccept: msg.VerifyOnAccept,
			CreatedAt:      ptrTime(createdAt),
			ReceivedAt:     receivedAt,
		}, nil
	case ContactControlActionAccept, ContactControlActionReject:
		var msg ContactDecisionControlMessage
		if err := decodeRPCJSON(raw, &msg); err != nil {
			return ContactControlEvent{}, WrapProtocolError(ErrInvalidParams, "contact request decision decode failed: %v", err)
		}
		respondedAt := normalizedNow(msg.RespondedAt)
		return ContactControlEvent{
			FromPeerID:     strings.TrimSpace(fromPeerID),
			ToPeerID:       strings.TrimSpace(msg.ToPeerID),
			Action:         msg.Action,
			RequestID:      strings.TrimSpace(msg.RequestID),
			VerifyOnAccept: msg.VerifyOnAccept,
			RespondedAt:    ptrTime(respondedAt),
			ReceivedAt:     receivedAt,
		}, nil
	default:
		return ContactControlEvent{}, WrapProtocolError(ErrInvalidParams, "unsupported contact control action: %s", header.Action)
	}
}

func EncodeContactRequestControlMessage(request ContactRequest) ([]byte, error) {
	request = normalizeContactRequest(request)
	if request.RequestID == "" {
		return nil, fmt.Errorf("request_id is required")
	}
	if request.InitiatorPeerID == "" {
		return nil, fmt.Errorf("initiator_peer_id is required")
	}
	if request.RecipientPeerID == "" {
		return nil, fmt.Errorf("recipient_peer_id is required")
	}
	cardRaw := []byte(strings.TrimSpace(request.InitiatorCardJSON))
	if len(cardRaw) == 0 {
		return nil, fmt.Errorf("initiator_card_json is required")
	}
	if err := mustJSONObject(cardRaw); err != nil {
		return nil, err
	}
	payload := ContactRequestControlMessage{
		Version:        ContactControlVersion,
		Action:         ContactControlActionRequest,
		RequestID:      request.RequestID,
		FromPeerID:     request.InitiatorPeerID,
		ToPeerID:       request.RecipientPeerID,
		VerifyOnAccept: request.VerifyOnAccept,
		CreatedAt:      request.CreatedAt,
		ContactCard:    json.RawMessage(cardRaw),
	}
	return json.Marshal(payload)
}

func EncodeContactDecisionControlMessage(request ContactRequest, action ContactControlAction, localCardRaw []byte, now time.Time) ([]byte, error) {
	request = normalizeContactRequest(request)
	if action != ContactControlActionAccept && action != ContactControlActionReject {
		return nil, fmt.Errorf("unsupported contact control action: %s", action)
	}
	if request.RequestID == "" {
		return nil, fmt.Errorf("request_id is required")
	}
	if request.InitiatorPeerID == "" {
		return nil, fmt.Errorf("initiator_peer_id is required")
	}
	if request.RecipientPeerID == "" {
		return nil, fmt.Errorf("recipient_peer_id is required")
	}
	payload := ContactDecisionControlMessage{
		Version:        ContactControlVersion,
		Action:         action,
		RequestID:      request.RequestID,
		FromPeerID:     request.RecipientPeerID,
		ToPeerID:       request.InitiatorPeerID,
		VerifyOnAccept: request.VerifyOnAccept,
		RespondedAt:    normalizedNow(now),
	}
	if action == ContactControlActionAccept {
		cardRaw := []byte(strings.TrimSpace(string(localCardRaw)))
		if len(cardRaw) == 0 {
			return nil, fmt.Errorf("contact_card is required for accept")
		}
		if err := mustJSONObject(cardRaw); err != nil {
			return nil, err
		}
		payload.ContactCard = json.RawMessage(cardRaw)
	}
	return json.Marshal(payload)
}

func (s *Service) ListContactRequests(ctx context.Context) ([]ContactRequest, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("nil aqua service")
	}
	return s.store.GetContactRequests(ctx)
}

func (s *Service) CreateContactRequest(ctx context.Context, recipientPeerID string, initiatorCardRaw []byte, displayName string, verifyOnAccept bool, now time.Time) (ContactRequest, error) {
	if s == nil || s.store == nil {
		return ContactRequest{}, fmt.Errorf("nil aqua service")
	}
	if err := s.store.Ensure(ctx); err != nil {
		return ContactRequest{}, err
	}
	now = normalizedNow(now)
	recipientPeerID = strings.TrimSpace(recipientPeerID)
	if err := validatePeerID(recipientPeerID); err != nil {
		return ContactRequest{}, err
	}
	local, err := s.localIdentity(ctx)
	if err != nil {
		return ContactRequest{}, err
	}
	if local.PeerID == recipientPeerID {
		return ContactRequest{}, fmt.Errorf("cannot add local peer as a contact request target")
	}
	parsed, err := ParseAndVerifyContactCard(initiatorCardRaw, now)
	if err != nil {
		return ContactRequest{}, err
	}
	if strings.TrimSpace(parsed.Card.Payload.PeerID) != local.PeerID {
		return ContactRequest{}, WrapProtocolError(ErrInvalidParams, "contact card peer_id does not match local identity")
	}

	requests, err := s.store.GetContactRequests(ctx)
	if err != nil {
		return ContactRequest{}, err
	}

	request := ContactRequest{
		RequestID:         "creq_" + uuid.NewString(),
		InitiatorPeerID:   local.PeerID,
		RecipientPeerID:   recipientPeerID,
		InitiatorCardJSON: strings.TrimSpace(string(initiatorCardRaw)),
		DisplayName:       strings.TrimSpace(displayName),
		VerifyOnAccept:    verifyOnAccept,
		Status:            ContactRequestStatusPending,
		CreatedAt:         now,
	}

	for _, existing := range requests {
		existing = normalizeContactRequest(existing)
		if existing.InitiatorPeerID != local.PeerID || existing.RecipientPeerID != recipientPeerID {
			continue
		}
		if existing.Status != ContactRequestStatusPending {
			continue
		}
		request.RequestID = existing.RequestID
		request.CreatedAt = existing.CreatedAt
		break
	}

	requests = upsertContactRequest(requests, request)
	if err := s.store.PutContactRequests(ctx, requests); err != nil {
		return ContactRequest{}, err
	}
	return request, nil
}

func (s *Service) ApplyInboundContactControl(ctx context.Context, fromPeerID string, raw []byte, now time.Time) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("nil aqua service")
	}
	if err := s.store.Ensure(ctx); err != nil {
		return err
	}
	var header contactControlHeader
	if err := decodeRPCJSON(raw, &header); err != nil {
		return WrapProtocolError(ErrInvalidParams, "contact control decode failed: %v", err)
	}
	if header.Version != ContactControlVersion {
		return WrapProtocolError(ErrInvalidParams, "contact control version must be %d", ContactControlVersion)
	}
	switch header.Action {
	case ContactControlActionRequest:
		var msg ContactRequestControlMessage
		if err := decodeRPCJSON(raw, &msg); err != nil {
			return WrapProtocolError(ErrInvalidParams, "contact request decode failed: %v", err)
		}
		return s.applyInboundContactRequest(ctx, strings.TrimSpace(fromPeerID), msg, normalizedNow(now))
	case ContactControlActionAccept, ContactControlActionReject:
		var msg ContactDecisionControlMessage
		if err := decodeRPCJSON(raw, &msg); err != nil {
			return WrapProtocolError(ErrInvalidParams, "contact request decision decode failed: %v", err)
		}
		return s.applyInboundContactDecision(ctx, strings.TrimSpace(fromPeerID), msg, normalizedNow(now))
	default:
		return WrapProtocolError(ErrInvalidParams, "unsupported contact control action: %s", header.Action)
	}
}

func (s *Service) AcceptContactRequest(ctx context.Context, identifier string, displayName string, now time.Time) (ContactRequest, Contact, error) {
	if s == nil || s.store == nil {
		return ContactRequest{}, Contact{}, fmt.Errorf("nil aqua service")
	}
	if err := s.store.Ensure(ctx); err != nil {
		return ContactRequest{}, Contact{}, err
	}
	now = normalizedNow(now)
	local, err := s.localIdentity(ctx)
	if err != nil {
		return ContactRequest{}, Contact{}, err
	}
	requests, err := s.store.GetContactRequests(ctx)
	if err != nil {
		return ContactRequest{}, Contact{}, err
	}
	index, request, err := resolveLocalContactRequest(requests, local.PeerID, identifier, true)
	if err != nil {
		return ContactRequest{}, Contact{}, err
	}
	effectiveDisplayName := strings.TrimSpace(displayName)
	if effectiveDisplayName == "" {
		effectiveDisplayName = request.DisplayName
	}

	switch request.Status {
	case ContactRequestStatusAccepted:
		contact, ok, err := s.GetContactByPeerID(ctx, request.InitiatorPeerID)
		if err != nil {
			return ContactRequest{}, Contact{}, err
		}
		if !ok {
			contact, err = s.importContactCardFromExpectedPeer(ctx, request.InitiatorPeerID, []byte(strings.TrimSpace(request.InitiatorCardJSON)), effectiveDisplayName, request.VerifyOnAccept, now)
			if err != nil {
				return ContactRequest{}, Contact{}, err
			}
		} else if request.VerifyOnAccept && contact.TrustState != TrustStateVerified {
			contact, err = s.MarkContactVerified(ctx, contact.PeerID, now)
			if err != nil {
				return ContactRequest{}, Contact{}, err
			}
		}
		updated := request
		if effectiveDisplayName != "" {
			updated.DisplayName = effectiveDisplayName
		}
		if updated.RespondedAt == nil {
			updated.RespondedAt = ptrTime(now)
		}
		requests[index] = updated
		if err := s.store.PutContactRequests(ctx, requests); err != nil {
			return ContactRequest{}, Contact{}, err
		}
		return updated, contact, nil
	case ContactRequestStatusPending:
	default:
		return request, Contact{}, fmt.Errorf("contact request is already terminal: %s", request.Status)
	}

	contact, err := s.importContactCardFromExpectedPeer(ctx, request.InitiatorPeerID, []byte(strings.TrimSpace(request.InitiatorCardJSON)), effectiveDisplayName, request.VerifyOnAccept, now)
	if err != nil {
		return ContactRequest{}, Contact{}, err
	}

	updated := request
	updated.DisplayName = effectiveDisplayName
	updated.Status = ContactRequestStatusAccepted
	updated.RespondedAt = ptrTime(now)
	requests[index] = updated
	if err := s.store.PutContactRequests(ctx, requests); err != nil {
		return ContactRequest{}, Contact{}, err
	}
	return updated, contact, nil
}

func (s *Service) RejectContactRequest(ctx context.Context, identifier string, now time.Time) (ContactRequest, error) {
	if s == nil || s.store == nil {
		return ContactRequest{}, fmt.Errorf("nil aqua service")
	}
	if err := s.store.Ensure(ctx); err != nil {
		return ContactRequest{}, err
	}
	now = normalizedNow(now)
	local, err := s.localIdentity(ctx)
	if err != nil {
		return ContactRequest{}, err
	}
	requests, err := s.store.GetContactRequests(ctx)
	if err != nil {
		return ContactRequest{}, err
	}
	index, request, err := resolveLocalContactRequest(requests, local.PeerID, identifier, true)
	if err != nil {
		return ContactRequest{}, err
	}
	switch request.Status {
	case ContactRequestStatusPending, ContactRequestStatusRejected:
	default:
		return request, fmt.Errorf("contact request is already terminal: %s", request.Status)
	}
	updated := request
	updated.Status = ContactRequestStatusRejected
	updated.RespondedAt = ptrTime(now)
	requests[index] = updated
	if err := s.store.PutContactRequests(ctx, requests); err != nil {
		return ContactRequest{}, err
	}
	return updated, nil
}

func (s *Service) applyInboundContactRequest(ctx context.Context, fromPeerID string, msg ContactRequestControlMessage, now time.Time) error {
	if err := validatePeerID(fromPeerID); err != nil {
		return WrapProtocolError(ErrUnauthorized, "%s", err.Error())
	}
	local, err := s.localIdentity(ctx)
	if err != nil {
		return err
	}
	requestID := strings.TrimSpace(msg.RequestID)
	initiatorPeerID := strings.TrimSpace(msg.FromPeerID)
	recipientPeerID := strings.TrimSpace(msg.ToPeerID)
	if requestID == "" {
		return WrapProtocolError(ErrInvalidParams, "request_id is required")
	}
	if initiatorPeerID == "" {
		initiatorPeerID = fromPeerID
	}
	if initiatorPeerID != fromPeerID {
		return WrapProtocolError(ErrUnauthorized, "transport sender does not match from_peer_id")
	}
	if err := validatePeerID(initiatorPeerID); err != nil {
		return WrapProtocolError(ErrInvalidParams, "%s", err.Error())
	}
	if err := validatePeerID(recipientPeerID); err != nil {
		return WrapProtocolError(ErrInvalidParams, "%s", err.Error())
	}
	if local.PeerID != recipientPeerID {
		return WrapProtocolError(ErrUnauthorized, "local peer is not request recipient")
	}
	cardRaw := json.RawMessage(append([]byte(nil), msg.ContactCard...))
	if len(cardRaw) == 0 {
		return WrapProtocolError(ErrInvalidParams, "contact_card is required")
	}
	parsed, err := ParseAndVerifyContactCard(cardRaw, now)
	if err != nil {
		return err
	}
	if strings.TrimSpace(parsed.Card.Payload.PeerID) != initiatorPeerID {
		return WrapProtocolError(ErrUnauthorized, "contact_card peer_id does not match request initiator")
	}

	requests, err := s.store.GetContactRequests(ctx)
	if err != nil {
		return err
	}
	for _, existing := range requests {
		existing = normalizeContactRequest(existing)
		if existing.RequestID != requestID {
			continue
		}
		if existing.InitiatorPeerID != initiatorPeerID || existing.RecipientPeerID != recipientPeerID {
			return WrapProtocolError(ErrInvalidParams, "request_id already exists with different peers")
		}
		if existing.Status != ContactRequestStatusPending {
			return nil
		}
	}

	target := ContactRequest{
		RequestID:         requestID,
		InitiatorPeerID:   initiatorPeerID,
		RecipientPeerID:   recipientPeerID,
		InitiatorCardJSON: strings.TrimSpace(string(cardRaw)),
		VerifyOnAccept:    msg.VerifyOnAccept,
		Status:            ContactRequestStatusPending,
		CreatedAt:         normalizedNow(msg.CreatedAt),
	}
	if target.CreatedAt.IsZero() {
		target.CreatedAt = now
	}
	requests = upsertContactRequest(requests, target)
	return s.store.PutContactRequests(ctx, requests)
}

func (s *Service) applyInboundContactDecision(ctx context.Context, fromPeerID string, msg ContactDecisionControlMessage, now time.Time) error {
	if err := validatePeerID(fromPeerID); err != nil {
		return WrapProtocolError(ErrUnauthorized, "%s", err.Error())
	}
	local, err := s.localIdentity(ctx)
	if err != nil {
		return err
	}
	requestID := strings.TrimSpace(msg.RequestID)
	fromPeerIDField := strings.TrimSpace(msg.FromPeerID)
	toPeerIDField := strings.TrimSpace(msg.ToPeerID)
	if requestID == "" {
		return WrapProtocolError(ErrInvalidParams, "request_id is required")
	}
	if fromPeerIDField == "" {
		fromPeerIDField = fromPeerID
	}
	if fromPeerIDField != fromPeerID {
		return WrapProtocolError(ErrUnauthorized, "transport sender does not match from_peer_id")
	}
	if err := validatePeerID(fromPeerIDField); err != nil {
		return WrapProtocolError(ErrInvalidParams, "%s", err.Error())
	}

	requests, err := s.store.GetContactRequests(ctx)
	if err != nil {
		return err
	}
	index := -1
	request := ContactRequest{}
	for i := range requests {
		candidate := normalizeContactRequest(requests[i])
		if candidate.RequestID != requestID {
			continue
		}
		index = i
		request = candidate
		break
	}
	if index < 0 {
		return WrapProtocolError(ErrInvalidParams, "contact request not found: %s", requestID)
	}
	if local.PeerID != request.InitiatorPeerID {
		return WrapProtocolError(ErrUnauthorized, "local peer is not request initiator")
	}
	if request.RecipientPeerID != fromPeerIDField {
		return WrapProtocolError(ErrUnauthorized, "transport sender does not match stored request recipient")
	}
	if toPeerIDField != "" && toPeerIDField != local.PeerID {
		return WrapProtocolError(ErrUnauthorized, "local peer is not request decision recipient")
	}

	target := ContactRequestStatusRejected
	switch msg.Action {
	case ContactControlActionAccept:
		target = ContactRequestStatusAccepted
	case ContactControlActionReject:
		target = ContactRequestStatusRejected
	default:
		return WrapProtocolError(ErrInvalidParams, "unsupported contact control action: %s", msg.Action)
	}

	if request.Status == target {
		return nil
	}
	if request.Status != ContactRequestStatusPending {
		return WrapProtocolError(ErrInvalidParams, "contact request is already terminal: %s", request.Status)
	}

	if target == ContactRequestStatusAccepted {
		cardRaw := json.RawMessage(append([]byte(nil), msg.ContactCard...))
		if len(cardRaw) == 0 {
			return WrapProtocolError(ErrInvalidParams, "contact_card is required for accept")
		}
		parsed, err := ParseAndVerifyContactCard(cardRaw, now)
		if err != nil {
			return err
		}
		if strings.TrimSpace(parsed.Card.Payload.PeerID) != request.RecipientPeerID {
			return WrapProtocolError(ErrUnauthorized, "contact_card peer_id does not match request recipient")
		}
		if _, err := s.importContactCardFromExpectedPeer(ctx, request.RecipientPeerID, cardRaw, request.DisplayName, request.VerifyOnAccept, now); err != nil {
			return err
		}
	}

	updated := request
	updated.Status = target
	respondedAt := normalizedNow(msg.RespondedAt)
	if respondedAt.IsZero() {
		respondedAt = now
	}
	updated.RespondedAt = ptrTime(respondedAt)
	requests[index] = updated
	return s.store.PutContactRequests(ctx, requests)
}

func resolveLocalContactRequest(requests []ContactRequest, localPeerID string, identifier string, incoming bool) (int, ContactRequest, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return -1, ContactRequest{}, fmt.Errorf("request_id or peer_id is required")
	}
	exactIndex := -1
	for i := range requests {
		request := normalizeContactRequest(requests[i])
		if !requestMatchesDirection(request, localPeerID, incoming) {
			continue
		}
		if request.RequestID == identifier {
			exactIndex = i
			break
		}
	}
	if exactIndex >= 0 {
		return exactIndex, normalizeContactRequest(requests[exactIndex]), nil
	}

	peerMatches := make([]int, 0, 2)
	pendingMatches := make([]int, 0, 2)
	for i := range requests {
		request := normalizeContactRequest(requests[i])
		if !requestMatchesDirection(request, localPeerID, incoming) {
			continue
		}
		if remotePeerIDForDirection(request, incoming) != identifier {
			continue
		}
		peerMatches = append(peerMatches, i)
		if request.Status == ContactRequestStatusPending {
			pendingMatches = append(pendingMatches, i)
		}
	}
	switch len(pendingMatches) {
	case 1:
		return pendingMatches[0], normalizeContactRequest(requests[pendingMatches[0]]), nil
	case 0:
	default:
		return -1, ContactRequest{}, fmt.Errorf("multiple pending contact requests for peer %s; specify request_id", identifier)
	}
	switch len(peerMatches) {
	case 0:
		direction := "outgoing"
		if incoming {
			direction = "incoming"
		}
		return -1, ContactRequest{}, fmt.Errorf("no %s contact request found for %s", direction, identifier)
	case 1:
		return peerMatches[0], normalizeContactRequest(requests[peerMatches[0]]), nil
	default:
		return -1, ContactRequest{}, fmt.Errorf("multiple contact requests found for peer %s; specify request_id", identifier)
	}
}

func requestMatchesDirection(request ContactRequest, localPeerID string, incoming bool) bool {
	request = normalizeContactRequest(request)
	localPeerID = strings.TrimSpace(localPeerID)
	if incoming {
		return request.RecipientPeerID == localPeerID
	}
	return request.InitiatorPeerID == localPeerID
}

func remotePeerIDForDirection(request ContactRequest, incoming bool) string {
	request = normalizeContactRequest(request)
	if incoming {
		return request.InitiatorPeerID
	}
	return request.RecipientPeerID
}

func upsertContactRequest(requests []ContactRequest, target ContactRequest) []ContactRequest {
	target = normalizeContactRequest(target)
	out := make([]ContactRequest, 0, len(requests)+1)
	replaced := false
	for _, request := range requests {
		request = normalizeContactRequest(request)
		if request.RequestID == target.RequestID {
			if target.DisplayName == "" {
				target.DisplayName = request.DisplayName
			}
			out = append(out, target)
			replaced = true
			continue
		}
		out = append(out, request)
	}
	if !replaced {
		out = append(out, target)
	}
	return out
}

func normalizeContactRequest(request ContactRequest) ContactRequest {
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.InitiatorPeerID = strings.TrimSpace(request.InitiatorPeerID)
	request.RecipientPeerID = strings.TrimSpace(request.RecipientPeerID)
	request.InitiatorCardJSON = strings.TrimSpace(request.InitiatorCardJSON)
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	request.Status = ContactRequestStatus(strings.TrimSpace(string(request.Status)))
	switch request.Status {
	case ContactRequestStatusPending, ContactRequestStatusAccepted, ContactRequestStatusRejected:
	default:
		request.Status = ContactRequestStatusPending
	}
	request.CreatedAt = request.CreatedAt.UTC()
	if request.RespondedAt != nil {
		at := request.RespondedAt.UTC()
		request.RespondedAt = &at
	}
	return request
}

func (s *Service) importContactCardFromExpectedPeer(ctx context.Context, expectedPeerID string, rawCard []byte, displayName string, verify bool, now time.Time) (Contact, error) {
	parsed, err := ParseAndVerifyContactCard(rawCard, now)
	if err != nil {
		return Contact{}, err
	}
	if strings.TrimSpace(parsed.Card.Payload.PeerID) != strings.TrimSpace(expectedPeerID) {
		return Contact{}, WrapProtocolError(ErrInvalidParams, "contact card peer_id mismatch")
	}
	result, err := s.ImportContactCard(ctx, rawCard, displayName, now)
	if err != nil {
		return Contact{}, err
	}
	contact := result.Contact
	if verify {
		contact, err = s.MarkContactVerified(ctx, contact.PeerID, now)
		if err != nil {
			return Contact{}, err
		}
	}
	return contact, nil
}
