package aqua

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p/core/peer"
)

func (s *Service) CreateGroup(ctx context.Context, now time.Time) (Group, error) {
	if s == nil || s.store == nil {
		return Group{}, fmt.Errorf("nil aqua service")
	}
	if err := s.store.Ensure(ctx); err != nil {
		return Group{}, err
	}
	local, err := s.localIdentity(ctx)
	if err != nil {
		return Group{}, err
	}
	now = normalizedNow(now)

	group := Group{
		GroupID:        "grp_" + uuid.NewString(),
		Epoch:          1,
		MaxMembers:     GroupMaxMembers,
		ManagerPeerIDs: []string{local.PeerID},
		LocalRole:      GroupRoleManager,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	roleState := GroupRoleState{
		GroupID:     group.GroupID,
		RoleVersion: 1,
		Roles: []GroupRoleEntry{
			{
				PeerID:    local.PeerID,
				Role:      GroupRoleManager,
				UpdatedAt: now,
			},
		},
	}
	members := []GroupMember{
		{
			GroupID:   group.GroupID,
			PeerID:    local.PeerID,
			JoinedAt:  now,
			UpdatedAt: now,
		},
	}

	if err := s.store.PutGroup(ctx, group); err != nil {
		return Group{}, err
	}
	if err := s.store.PutGroupRoleState(ctx, roleState); err != nil {
		return Group{}, err
	}
	if err := s.store.PutGroupMembers(ctx, group.GroupID, members); err != nil {
		return Group{}, err
	}
	return group, nil
}

func (s *Service) InviteGroupMember(ctx context.Context, groupID string, inviteePeerID string, now time.Time) (GroupInvite, error) {
	if s == nil || s.store == nil {
		return GroupInvite{}, fmt.Errorf("nil aqua service")
	}
	now = normalizedNow(now)
	groupID = strings.TrimSpace(groupID)
	inviteePeerID = strings.TrimSpace(inviteePeerID)
	if groupID == "" {
		return GroupInvite{}, fmt.Errorf("group_id is required")
	}
	if err := validatePeerID(inviteePeerID); err != nil {
		return GroupInvite{}, err
	}
	local, err := s.localIdentity(ctx)
	if err != nil {
		return GroupInvite{}, err
	}

	group, roleState, members, invites, err := s.loadGroupState(ctx, groupID)
	if err != nil {
		return GroupInvite{}, err
	}
	if !isManagerPeer(roleState, local.PeerID) {
		return GroupInvite{}, fmt.Errorf("group %s requires manager role", groupID)
	}
	if hasMemberPeer(members, inviteePeerID) {
		return GroupInvite{}, fmt.Errorf("peer is already a group member: %s", inviteePeerID)
	}
	if len(members) >= group.MaxMembers {
		return GroupInvite{}, fmt.Errorf("group member limit reached: %d", group.MaxMembers)
	}
	for _, invite := range invites {
		if strings.TrimSpace(invite.InviteePeerID) != inviteePeerID {
			continue
		}
		if invite.Status != GroupInviteStatusPending {
			continue
		}
		if invite.ExpiresAt.Add(GroupInviteClockSkew).After(now) {
			return invite, nil
		}
	}

	inviteID, err := generateInviteID()
	if err != nil {
		return GroupInvite{}, err
	}
	invite := GroupInvite{
		GroupID:       groupID,
		InviteID:      inviteID,
		InviterPeerID: local.PeerID,
		InviteePeerID: inviteePeerID,
		CreatedAt:     now,
		ExpiresAt:     now.Add(GroupInviteTTL),
		Status:        GroupInviteStatusPending,
	}
	invites = append(invites, invite)
	if err := s.store.PutGroupInvites(ctx, groupID, invites); err != nil {
		return GroupInvite{}, err
	}
	return invite, nil
}

func (s *Service) AcceptGroupInvite(ctx context.Context, groupID string, inviteID string, now time.Time) (GroupInvite, Group, error) {
	return s.resolveGroupInvite(ctx, groupID, inviteID, GroupInviteStatusAccepted, now)
}

func (s *Service) RejectGroupInvite(ctx context.Context, groupID string, inviteID string, now time.Time) (GroupInvite, error) {
	invite, _, err := s.resolveGroupInvite(ctx, groupID, inviteID, GroupInviteStatusRejected, now)
	return invite, err
}

func (s *Service) RemoveGroupMember(ctx context.Context, groupID string, peerID string, now time.Time) (Group, error) {
	if s == nil || s.store == nil {
		return Group{}, fmt.Errorf("nil aqua service")
	}
	now = normalizedNow(now)
	groupID = strings.TrimSpace(groupID)
	peerID = strings.TrimSpace(peerID)
	if groupID == "" {
		return Group{}, fmt.Errorf("group_id is required")
	}
	if err := validatePeerID(peerID); err != nil {
		return Group{}, err
	}
	local, err := s.localIdentity(ctx)
	if err != nil {
		return Group{}, err
	}

	group, roleState, members, _, err := s.loadGroupState(ctx, groupID)
	if err != nil {
		return Group{}, err
	}
	if !isManagerPeer(roleState, local.PeerID) {
		return Group{}, fmt.Errorf("group %s requires manager role", groupID)
	}
	if !hasMemberPeer(members, peerID) {
		return Group{}, fmt.Errorf("peer is not an active group member: %s", peerID)
	}

	updatedMembers := make([]GroupMember, 0, len(members))
	for _, member := range members {
		if strings.TrimSpace(member.PeerID) == peerID {
			continue
		}
		updatedMembers = append(updatedMembers, member)
	}
	if len(updatedMembers) == len(members) {
		return group, nil
	}

	updatedRoleState, changedRole := removePeerFromRoleState(roleState, peerID, now)
	if changedRole && managerCount(updatedRoleState) == 0 {
		return Group{}, fmt.Errorf("cannot remove last manager")
	}
	if !changedRole {
		updatedRoleState = ensureRoleForMembers(updatedRoleState, updatedMembers, now)
	}
	updatedManagers := collectManagerPeerIDs(updatedRoleState)

	group.Epoch++
	group.ManagerPeerIDs = updatedManagers
	group.LocalRole = resolveLocalRole(local.PeerID, updatedRoleState, updatedMembers)
	group.UpdatedAt = now

	if err := s.store.PutGroupMembers(ctx, groupID, updatedMembers); err != nil {
		return Group{}, err
	}
	if err := s.store.PutGroupRoleState(ctx, updatedRoleState); err != nil {
		return Group{}, err
	}
	if err := s.store.PutGroup(ctx, group); err != nil {
		return Group{}, err
	}
	return group, nil
}

func (s *Service) SetGroupRole(ctx context.Context, groupID string, peerID string, role GroupRole, now time.Time) (GroupRoleState, Group, error) {
	if s == nil || s.store == nil {
		return GroupRoleState{}, Group{}, fmt.Errorf("nil aqua service")
	}
	now = normalizedNow(now)
	groupID = strings.TrimSpace(groupID)
	peerID = strings.TrimSpace(peerID)
	if groupID == "" {
		return GroupRoleState{}, Group{}, fmt.Errorf("group_id is required")
	}
	if err := validatePeerID(peerID); err != nil {
		return GroupRoleState{}, Group{}, err
	}
	parsedRole, err := ParseGroupRole(string(role))
	if err != nil {
		return GroupRoleState{}, Group{}, err
	}
	local, err := s.localIdentity(ctx)
	if err != nil {
		return GroupRoleState{}, Group{}, err
	}

	group, roleState, members, _, err := s.loadGroupState(ctx, groupID)
	if err != nil {
		return GroupRoleState{}, Group{}, err
	}
	if !isManagerPeer(roleState, local.PeerID) {
		return GroupRoleState{}, Group{}, fmt.Errorf("group %s requires manager role", groupID)
	}
	if !hasMemberPeer(members, peerID) {
		return GroupRoleState{}, Group{}, fmt.Errorf("peer is not an active group member: %s", peerID)
	}

	currentRole, ok := roleOfPeer(roleState, peerID)
	if !ok {
		currentRole = GroupRoleMember
	}
	if currentRole == parsedRole {
		group.LocalRole = resolveLocalRole(local.PeerID, roleState, members)
		return roleState, group, nil
	}

	updatedRoleState := roleState
	updatedRoleState.GroupID = groupID
	updatedRoleState.RoleVersion++
	updatedRoleState.Roles = upsertRoleEntry(roleState.Roles, GroupRoleEntry{
		PeerID:    peerID,
		Role:      parsedRole,
		UpdatedAt: now,
	})
	if managerCount(updatedRoleState) == 0 {
		return GroupRoleState{}, Group{}, fmt.Errorf("cannot demote last manager")
	}
	updatedRoleState = ensureRoleForMembers(updatedRoleState, members, now)
	group.ManagerPeerIDs = collectManagerPeerIDs(updatedRoleState)
	group.LocalRole = resolveLocalRole(local.PeerID, updatedRoleState, members)
	group.UpdatedAt = now

	if err := s.store.PutGroupRoleState(ctx, updatedRoleState); err != nil {
		return GroupRoleState{}, Group{}, err
	}
	if err := s.store.PutGroup(ctx, group); err != nil {
		return GroupRoleState{}, Group{}, err
	}
	return updatedRoleState, group, nil
}

func (s *Service) ListGroups(ctx context.Context) ([]Group, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("nil aqua service")
	}
	groups, err := s.store.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].UpdatedAt.After(groups[j].UpdatedAt)
	})
	return groups, nil
}

func (s *Service) GetGroupDetails(ctx context.Context, groupID string) (GroupDetails, error) {
	if s == nil || s.store == nil {
		return GroupDetails{}, fmt.Errorf("nil aqua service")
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return GroupDetails{}, fmt.Errorf("group_id is required")
	}
	group, roleState, members, invites, err := s.loadGroupState(ctx, groupID)
	if err != nil {
		return GroupDetails{}, err
	}
	return GroupDetails{
		Group:     group,
		RoleState: roleState,
		Members:   members,
		Invites:   invites,
	}, nil
}

func (s *Service) localIdentity(ctx context.Context) (Identity, error) {
	identity, ok, err := s.GetIdentity(ctx)
	if err != nil {
		return Identity{}, err
	}
	if !ok {
		return Identity{}, fmt.Errorf("identity is not initialized; run `aqua init`")
	}
	return identity, nil
}

func (s *Service) loadGroupState(ctx context.Context, groupID string) (Group, GroupRoleState, []GroupMember, []GroupInvite, error) {
	group, ok, err := s.store.GetGroup(ctx, groupID)
	if err != nil {
		return Group{}, GroupRoleState{}, nil, nil, err
	}
	if !ok {
		return Group{}, GroupRoleState{}, nil, nil, fmt.Errorf("group not found: %s", groupID)
	}

	roleState, ok, err := s.store.GetGroupRoleState(ctx, groupID)
	if err != nil {
		return Group{}, GroupRoleState{}, nil, nil, err
	}
	if !ok {
		roleState = GroupRoleState{
			GroupID:     groupID,
			RoleVersion: 1,
			Roles:       nil,
		}
		for _, managerPeerID := range group.ManagerPeerIDs {
			roleState.Roles = append(roleState.Roles, GroupRoleEntry{
				PeerID: managerPeerID,
				Role:   GroupRoleManager,
			})
		}
	}

	members, err := s.store.GetGroupMembers(ctx, groupID)
	if err != nil {
		return Group{}, GroupRoleState{}, nil, nil, err
	}
	invites, err := s.store.GetGroupInvites(ctx, groupID)
	if err != nil {
		return Group{}, GroupRoleState{}, nil, nil, err
	}

	roleState = ensureRoleForMembers(roleState, members, time.Now().UTC())
	group.ManagerPeerIDs = collectManagerPeerIDs(roleState)
	return group, roleState, members, invites, nil
}

func (s *Service) resolveGroupInvite(ctx context.Context, groupID string, inviteID string, target GroupInviteStatus, now time.Time) (GroupInvite, Group, error) {
	if s == nil || s.store == nil {
		return GroupInvite{}, Group{}, fmt.Errorf("nil aqua service")
	}
	now = normalizedNow(now)
	groupID = strings.TrimSpace(groupID)
	inviteID = strings.TrimSpace(inviteID)
	if groupID == "" {
		return GroupInvite{}, Group{}, fmt.Errorf("group_id is required")
	}
	if inviteID == "" {
		return GroupInvite{}, Group{}, fmt.Errorf("invite_id is required")
	}
	if target != GroupInviteStatusAccepted && target != GroupInviteStatusRejected {
		return GroupInvite{}, Group{}, fmt.Errorf("unsupported invite target status: %s", target)
	}
	local, err := s.localIdentity(ctx)
	if err != nil {
		return GroupInvite{}, Group{}, err
	}
	group, roleState, members, invites, err := s.loadGroupState(ctx, groupID)
	if err != nil {
		return GroupInvite{}, Group{}, err
	}

	found := -1
	for i := range invites {
		if strings.TrimSpace(invites[i].InviteID) == inviteID {
			found = i
			break
		}
	}
	if found < 0 {
		return GroupInvite{}, Group{}, fmt.Errorf("invite not found: %s", inviteID)
	}
	invite := invites[found]
	if local.PeerID != invite.InviteePeerID && !isManagerPeer(roleState, local.PeerID) {
		return GroupInvite{}, Group{}, fmt.Errorf("invite can be resolved only by invitee or manager")
	}

	if invite.Status == target {
		return invite, group, nil
	}
	if invite.Status != GroupInviteStatusPending {
		return invite, group, fmt.Errorf("invite is already terminal: %s", invite.Status)
	}
	if invite.ExpiresAt.Add(GroupInviteClockSkew).Before(now) {
		invite.Status = GroupInviteStatusExpired
		invites[found] = invite
		_ = s.store.PutGroupInvites(ctx, groupID, invites)
		return invite, group, fmt.Errorf("invite expired")
	}

	invite.Status = target
	invite.RespondedAt = ptrTime(now)
	invites[found] = invite

	if target == GroupInviteStatusAccepted && !hasMemberPeer(members, invite.InviteePeerID) {
		if len(members) >= group.MaxMembers {
			return GroupInvite{}, Group{}, fmt.Errorf("group member limit reached: %d", group.MaxMembers)
		}
		members = append(members, GroupMember{
			GroupID:    groupID,
			PeerID:     invite.InviteePeerID,
			JoinedAt:   now,
			LastSeenAt: ptrTime(now),
			UpdatedAt:  now,
		})
		roleState = ensureRoleForMembers(roleState, members, now)
		group.Epoch++
		group.UpdatedAt = now
	}
	group.ManagerPeerIDs = collectManagerPeerIDs(roleState)
	group.LocalRole = resolveLocalRole(local.PeerID, roleState, members)

	if err := s.store.PutGroupInvites(ctx, groupID, invites); err != nil {
		return GroupInvite{}, Group{}, err
	}
	if err := s.store.PutGroupMembers(ctx, groupID, members); err != nil {
		return GroupInvite{}, Group{}, err
	}
	if err := s.store.PutGroupRoleState(ctx, roleState); err != nil {
		return GroupInvite{}, Group{}, err
	}
	if err := s.store.PutGroup(ctx, group); err != nil {
		return GroupInvite{}, Group{}, err
	}
	return invite, group, nil
}

func removePeerFromRoleState(state GroupRoleState, peerID string, now time.Time) (GroupRoleState, bool) {
	peerID = strings.TrimSpace(peerID)
	changed := false
	roles := make([]GroupRoleEntry, 0, len(state.Roles))
	for _, role := range state.Roles {
		if strings.TrimSpace(role.PeerID) == peerID {
			changed = true
			continue
		}
		roles = append(roles, role)
	}
	if !changed {
		return state, false
	}
	state.Roles = roles
	state.RoleVersion++
	if state.RoleVersion <= 0 {
		state.RoleVersion = 1
	}
	state = ensureRoleTimestamps(state, now)
	return state, true
}

func ensureRoleForMembers(state GroupRoleState, members []GroupMember, now time.Time) GroupRoleState {
	state.GroupID = strings.TrimSpace(state.GroupID)
	roleByPeer := map[string]GroupRoleEntry{}
	for _, role := range state.Roles {
		peerID := strings.TrimSpace(role.PeerID)
		if peerID == "" {
			continue
		}
		role.Role = GroupRole(strings.TrimSpace(string(role.Role)))
		if role.Role != GroupRoleManager && role.Role != GroupRoleMember {
			continue
		}
		roleByPeer[peerID] = role
	}
	for _, member := range members {
		peerID := strings.TrimSpace(member.PeerID)
		if peerID == "" {
			continue
		}
		if _, ok := roleByPeer[peerID]; ok {
			continue
		}
		roleByPeer[peerID] = GroupRoleEntry{
			PeerID:    peerID,
			Role:      GroupRoleMember,
			UpdatedAt: now,
		}
	}

	roles := make([]GroupRoleEntry, 0, len(roleByPeer))
	for _, role := range roleByPeer {
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool {
		return strings.TrimSpace(roles[i].PeerID) < strings.TrimSpace(roles[j].PeerID)
	})
	state.Roles = roles
	if state.RoleVersion <= 0 {
		state.RoleVersion = 1
	}
	return ensureRoleTimestamps(state, now)
}

func ensureRoleTimestamps(state GroupRoleState, now time.Time) GroupRoleState {
	now = normalizedNow(now)
	for i := range state.Roles {
		state.Roles[i].UpdatedAt = state.Roles[i].UpdatedAt.UTC()
		if state.Roles[i].UpdatedAt.IsZero() {
			state.Roles[i].UpdatedAt = now
		}
	}
	return state
}

func collectManagerPeerIDs(state GroupRoleState) []string {
	out := make([]string, 0, len(state.Roles))
	for _, role := range state.Roles {
		if role.Role != GroupRoleManager {
			continue
		}
		peerID := strings.TrimSpace(role.PeerID)
		if peerID == "" {
			continue
		}
		out = append(out, peerID)
	}
	sort.Strings(out)
	return dedupeSortedStrings(out)
}

func hasMemberPeer(members []GroupMember, peerID string) bool {
	peerID = strings.TrimSpace(peerID)
	for _, member := range members {
		if strings.TrimSpace(member.PeerID) == peerID {
			return true
		}
	}
	return false
}

func isManagerPeer(state GroupRoleState, peerID string) bool {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return false
	}
	for _, role := range state.Roles {
		if strings.TrimSpace(role.PeerID) == peerID && role.Role == GroupRoleManager {
			return true
		}
	}
	return false
}

func managerCount(state GroupRoleState) int {
	count := 0
	for _, role := range state.Roles {
		if role.Role == GroupRoleManager {
			count++
		}
	}
	return count
}

func roleOfPeer(state GroupRoleState, peerID string) (GroupRole, bool) {
	peerID = strings.TrimSpace(peerID)
	for _, role := range state.Roles {
		if strings.TrimSpace(role.PeerID) != peerID {
			continue
		}
		return role.Role, true
	}
	return "", false
}

func upsertRoleEntry(roles []GroupRoleEntry, target GroupRoleEntry) []GroupRoleEntry {
	target.PeerID = strings.TrimSpace(target.PeerID)
	replaced := false
	out := make([]GroupRoleEntry, 0, len(roles)+1)
	for _, role := range roles {
		if strings.TrimSpace(role.PeerID) == target.PeerID {
			out = append(out, target)
			replaced = true
			continue
		}
		out = append(out, role)
	}
	if !replaced {
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.TrimSpace(out[i].PeerID) < strings.TrimSpace(out[j].PeerID)
	})
	return out
}

func resolveLocalRole(localPeerID string, roleState GroupRoleState, members []GroupMember) GroupRole {
	if isManagerPeer(roleState, localPeerID) {
		return GroupRoleManager
	}
	if hasMemberPeer(members, localPeerID) {
		return GroupRoleMember
	}
	return ""
}

func validatePeerID(peerID string) error {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return fmt.Errorf("peer_id is required")
	}
	if _, err := peer.Decode(peerID); err != nil {
		return fmt.Errorf("invalid peer_id: %w", err)
	}
	return nil
}

func generateInviteID() (string, error) {
	var raw [16]byte
	if _, err := crand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate invite_id: %w", err)
	}
	return "inv_" + hex.EncodeToString(raw[:]), nil
}

func dedupeSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	last := ""
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == last {
			continue
		}
		out = append(out, value)
		last = value
	}
	return out
}

func ptrTime(v time.Time) *time.Time {
	if v.IsZero() {
		return nil
	}
	ts := v.UTC()
	return &ts
}
