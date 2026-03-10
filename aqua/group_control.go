package aqua

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const GroupControlVersion = 1

type GroupControlAction string

const (
	GroupControlActionInvite       GroupControlAction = "group.invite"
	GroupControlActionInviteAccept GroupControlAction = "group.invite.accept"
	GroupControlActionInviteReject GroupControlAction = "group.invite.reject"
)

type GroupInviteControlMessage struct {
	Version int                `json:"version"`
	Action  GroupControlAction `json:"action"`
	Invite  GroupInvite        `json:"invite"`
}

type GroupInviteDecisionMessage struct {
	Version       int                `json:"version"`
	Action        GroupControlAction `json:"action"`
	GroupID       string             `json:"group_id"`
	InviteID      string             `json:"invite_id"`
	InviterPeerID string             `json:"inviter_peer_id,omitempty"`
	InviteePeerID string             `json:"invitee_peer_id,omitempty"`
	RespondedAt   time.Time          `json:"responded_at"`
}

type groupControlHeader struct {
	Version int                `json:"version"`
	Action  GroupControlAction `json:"action"`
}

func EncodeGroupInviteControlMessage(invite GroupInvite) ([]byte, error) {
	payload := GroupInviteControlMessage{
		Version: GroupControlVersion,
		Action:  GroupControlActionInvite,
		Invite:  invite,
	}
	return json.Marshal(payload)
}

func EncodeGroupInviteDecisionMessage(invite GroupInvite, action GroupControlAction, now time.Time) ([]byte, error) {
	if action != GroupControlActionInviteAccept && action != GroupControlActionInviteReject {
		return nil, fmt.Errorf("unsupported group control action: %s", action)
	}
	payload := GroupInviteDecisionMessage{
		Version:       GroupControlVersion,
		Action:        action,
		GroupID:       strings.TrimSpace(invite.GroupID),
		InviteID:      strings.TrimSpace(invite.InviteID),
		InviterPeerID: strings.TrimSpace(invite.InviterPeerID),
		InviteePeerID: strings.TrimSpace(invite.InviteePeerID),
		RespondedAt:   normalizedNow(now),
	}
	return json.Marshal(payload)
}

func (s *Service) ListGroupInvites(ctx context.Context, groupID string) ([]GroupInvite, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("nil aqua service")
	}
	return s.store.GetGroupInvites(ctx, strings.TrimSpace(groupID))
}

func (s *Service) ApplyInboundGroupControl(ctx context.Context, fromPeerID string, raw []byte, now time.Time) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("nil aqua service")
	}
	if err := s.store.Ensure(ctx); err != nil {
		return err
	}
	var header groupControlHeader
	if err := decodeRPCJSON(raw, &header); err != nil {
		return WrapProtocolError(ErrInvalidParams, "group control decode failed: %v", err)
	}
	if header.Version != GroupControlVersion {
		return WrapProtocolError(ErrInvalidParams, "group control version must be %d", GroupControlVersion)
	}
	switch header.Action {
	case GroupControlActionInvite:
		var msg GroupInviteControlMessage
		if err := decodeRPCJSON(raw, &msg); err != nil {
			return WrapProtocolError(ErrInvalidParams, "group invite decode failed: %v", err)
		}
		return s.applyInboundGroupInvite(ctx, strings.TrimSpace(fromPeerID), msg, now)
	case GroupControlActionInviteAccept, GroupControlActionInviteReject:
		var msg GroupInviteDecisionMessage
		if err := decodeRPCJSON(raw, &msg); err != nil {
			return WrapProtocolError(ErrInvalidParams, "group invite decision decode failed: %v", err)
		}
		return s.applyInboundGroupInviteDecision(ctx, strings.TrimSpace(fromPeerID), msg, now)
	default:
		return WrapProtocolError(ErrInvalidParams, "unsupported group control action: %s", header.Action)
	}
}

func BuildGroupControlPushRequest(raw []byte, idempotencyKey string) DataPushRequest {
	return DataPushRequest{
		Topic:          GroupControlTopicV1,
		ContentType:    "application/json",
		PayloadBase64:  base64.RawURLEncoding.EncodeToString(raw),
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
	}
}

func (s *Service) applyInboundGroupInvite(ctx context.Context, fromPeerID string, msg GroupInviteControlMessage, now time.Time) error {
	now = normalizedNow(now)
	if err := validatePeerID(fromPeerID); err != nil {
		return WrapProtocolError(ErrUnauthorized, "%s", err.Error())
	}
	local, err := s.localIdentity(ctx)
	if err != nil {
		return err
	}

	invite := msg.Invite
	invite.GroupID = strings.TrimSpace(invite.GroupID)
	invite.InviteID = strings.TrimSpace(invite.InviteID)
	invite.InviterPeerID = strings.TrimSpace(invite.InviterPeerID)
	invite.InviteePeerID = strings.TrimSpace(invite.InviteePeerID)
	if invite.GroupID == "" {
		return WrapProtocolError(ErrInvalidParams, "group_id is required")
	}
	if invite.InviteID == "" {
		return WrapProtocolError(ErrInvalidParams, "invite_id is required")
	}
	if invite.InviterPeerID == "" {
		invite.InviterPeerID = fromPeerID
	}
	if invite.InviterPeerID != fromPeerID {
		return WrapProtocolError(ErrUnauthorized, "transport sender does not match inviter_peer_id")
	}
	if err := validatePeerID(invite.InviterPeerID); err != nil {
		return WrapProtocolError(ErrInvalidParams, "%s", err.Error())
	}
	if err := validatePeerID(invite.InviteePeerID); err != nil {
		return WrapProtocolError(ErrInvalidParams, "%s", err.Error())
	}
	if local.PeerID != invite.InviteePeerID {
		return WrapProtocolError(ErrUnauthorized, "local peer is not invitee")
	}
	if invite.ExpiresAt.IsZero() {
		invite.ExpiresAt = invite.CreatedAt.Add(GroupInviteTTL)
	}
	if invite.ExpiresAt.Add(GroupInviteClockSkew).Before(now) {
		return WrapProtocolError(ErrInvalidParams, "invite expired")
	}
	if invite.Status == "" {
		invite.Status = GroupInviteStatusPending
	}
	if invite.Status != GroupInviteStatusPending {
		return WrapProtocolError(ErrInvalidParams, "group.invite status must be pending")
	}
	invite.RespondedAt = nil

	existingGroup, foundGroup, err := s.store.GetGroup(ctx, invite.GroupID)
	if err != nil {
		return err
	}
	if foundGroup && strings.TrimSpace(string(existingGroup.LocalRole)) != "" {
		return WrapProtocolError(ErrInvalidParams, "local peer is already an active member")
	}

	invites, err := s.store.GetGroupInvites(ctx, invite.GroupID)
	if err != nil {
		return err
	}
	invites = upsertInboundInvite(invites, invite)
	return s.store.PutGroupInvites(ctx, invite.GroupID, invites)
}

func (s *Service) applyInboundGroupInviteDecision(ctx context.Context, fromPeerID string, msg GroupInviteDecisionMessage, now time.Time) error {
	now = normalizedNow(now)
	if err := validatePeerID(fromPeerID); err != nil {
		return WrapProtocolError(ErrUnauthorized, "%s", err.Error())
	}
	local, err := s.localIdentity(ctx)
	if err != nil {
		return err
	}
	msg.GroupID = strings.TrimSpace(msg.GroupID)
	msg.InviteID = strings.TrimSpace(msg.InviteID)
	msg.InviterPeerID = strings.TrimSpace(msg.InviterPeerID)
	msg.InviteePeerID = strings.TrimSpace(msg.InviteePeerID)
	if msg.GroupID == "" {
		return WrapProtocolError(ErrInvalidParams, "group_id is required")
	}
	if msg.InviteID == "" {
		return WrapProtocolError(ErrInvalidParams, "invite_id is required")
	}
	if msg.InviteePeerID != "" && msg.InviteePeerID != fromPeerID {
		return WrapProtocolError(ErrUnauthorized, "transport sender does not match invitee_peer_id")
	}
	if msg.InviterPeerID != "" && msg.InviterPeerID != local.PeerID {
		return WrapProtocolError(ErrUnauthorized, "local peer is not inviter")
	}

	group, roleState, _, invites, err := s.loadGroupState(ctx, msg.GroupID)
	if err != nil {
		return err
	}
	if !isManagerPeer(roleState, local.PeerID) {
		return WrapProtocolError(ErrUnauthorized, "local peer is not a group manager")
	}
	found := -1
	for i := range invites {
		if strings.TrimSpace(invites[i].InviteID) == msg.InviteID {
			found = i
			break
		}
	}
	if found < 0 {
		return WrapProtocolError(ErrInvalidParams, "invite not found: %s", msg.InviteID)
	}
	invite := invites[found]
	if fromPeerID != invite.InviteePeerID {
		return WrapProtocolError(ErrUnauthorized, "transport sender does not match stored invitee")
	}
	if msg.InviterPeerID != "" && msg.InviterPeerID != invite.InviterPeerID {
		return WrapProtocolError(ErrInvalidParams, "inviter_peer_id mismatch")
	}
	_ = group

	target := GroupInviteStatusRejected
	switch msg.Action {
	case GroupControlActionInviteAccept:
		target = GroupInviteStatusAccepted
	case GroupControlActionInviteReject:
		target = GroupInviteStatusRejected
	default:
		return WrapProtocolError(ErrInvalidParams, "unsupported group control action: %s", msg.Action)
	}
	_, _, err = s.resolveGroupInvite(ctx, msg.GroupID, msg.InviteID, target, now)
	return err
}

func upsertInboundInvite(invites []GroupInvite, target GroupInvite) []GroupInvite {
	target.GroupID = strings.TrimSpace(target.GroupID)
	target.InviteID = strings.TrimSpace(target.InviteID)
	target.InviterPeerID = strings.TrimSpace(target.InviterPeerID)
	target.InviteePeerID = strings.TrimSpace(target.InviteePeerID)
	target.CreatedAt = target.CreatedAt.UTC()
	target.ExpiresAt = target.ExpiresAt.UTC()
	if target.RespondedAt != nil {
		at := target.RespondedAt.UTC()
		target.RespondedAt = &at
	}
	out := make([]GroupInvite, 0, len(invites)+1)
	replaced := false
	for _, invite := range invites {
		if strings.TrimSpace(invite.InviteID) != target.InviteID {
			out = append(out, invite)
			continue
		}
		if invite.Status != GroupInviteStatusPending {
			out = append(out, invite)
			replaced = true
			continue
		}
		out = append(out, target)
		replaced = true
	}
	if !replaced {
		out = append(out, target)
	}
	return out
}
