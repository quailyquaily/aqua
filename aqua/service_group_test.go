package aqua

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestServiceCreateGroup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 2, 24, 9, 0, 0, 0, time.UTC)
	store := NewFileStore(t.TempDir())
	svc := NewService(store)

	identity, _, err := svc.EnsureIdentity(ctx, now)
	if err != nil {
		t.Fatalf("EnsureIdentity() error = %v", err)
	}

	group, err := svc.CreateGroup(ctx, now)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	if strings.TrimSpace(group.GroupID) == "" {
		t.Fatalf("group_id should not be empty")
	}
	if group.Epoch != 1 {
		t.Fatalf("group epoch mismatch: got %d want 1", group.Epoch)
	}
	if group.MaxMembers != GroupMaxMembers {
		t.Fatalf("group max_members mismatch: got %d want %d", group.MaxMembers, GroupMaxMembers)
	}
	if group.LocalRole != GroupRoleManager {
		t.Fatalf("group local_role mismatch: got %s want %s", group.LocalRole, GroupRoleManager)
	}
	if len(group.ManagerPeerIDs) != 1 || group.ManagerPeerIDs[0] != identity.PeerID {
		t.Fatalf("group manager_peer_ids mismatch: got %v want [%s]", group.ManagerPeerIDs, identity.PeerID)
	}

	details, err := svc.GetGroupDetails(ctx, group.GroupID)
	if err != nil {
		t.Fatalf("GetGroupDetails() error = %v", err)
	}
	if details.RoleState.RoleVersion != 1 {
		t.Fatalf("role_version mismatch: got %d want 1", details.RoleState.RoleVersion)
	}
	if len(details.Members) != 1 || details.Members[0].PeerID != identity.PeerID {
		t.Fatalf("members mismatch: got %+v", details.Members)
	}
}

func TestServiceGroupInviteAcceptReject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 2, 24, 10, 0, 0, 0, time.UTC)
	store := NewFileStore(t.TempDir())
	svc := NewService(store)

	_, _, err := svc.EnsureIdentity(ctx, now)
	if err != nil {
		t.Fatalf("EnsureIdentity() error = %v", err)
	}
	group, err := svc.CreateGroup(ctx, now)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}

	inviteeA, err := GenerateIdentity(now.Add(time.Minute))
	if err != nil {
		t.Fatalf("GenerateIdentity(inviteeA) error = %v", err)
	}
	inviteA, err := svc.InviteGroupMember(ctx, group.GroupID, inviteeA.PeerID, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("InviteGroupMember(A) error = %v", err)
	}
	if inviteA.Status != GroupInviteStatusPending {
		t.Fatalf("inviteA status mismatch: got %s want %s", inviteA.Status, GroupInviteStatusPending)
	}

	accepted, updatedGroup, err := svc.AcceptGroupInvite(ctx, group.GroupID, inviteA.InviteID, now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("AcceptGroupInvite() error = %v", err)
	}
	if accepted.Status != GroupInviteStatusAccepted {
		t.Fatalf("accepted status mismatch: got %s want %s", accepted.Status, GroupInviteStatusAccepted)
	}
	if updatedGroup.Epoch != 2 {
		t.Fatalf("group epoch mismatch after accept: got %d want 2", updatedGroup.Epoch)
	}

	acceptedAgain, groupAgain, err := svc.AcceptGroupInvite(ctx, group.GroupID, inviteA.InviteID, now.Add(4*time.Minute))
	if err != nil {
		t.Fatalf("AcceptGroupInvite(idempotent) error = %v", err)
	}
	if acceptedAgain.Status != GroupInviteStatusAccepted {
		t.Fatalf("acceptedAgain status mismatch: got %s want %s", acceptedAgain.Status, GroupInviteStatusAccepted)
	}
	if groupAgain.Epoch != 2 {
		t.Fatalf("group epoch should remain unchanged on idempotent accept: got %d want 2", groupAgain.Epoch)
	}

	inviteeB, err := GenerateIdentity(now.Add(5 * time.Minute))
	if err != nil {
		t.Fatalf("GenerateIdentity(inviteeB) error = %v", err)
	}
	inviteB, err := svc.InviteGroupMember(ctx, group.GroupID, inviteeB.PeerID, now.Add(6*time.Minute))
	if err != nil {
		t.Fatalf("InviteGroupMember(B) error = %v", err)
	}
	rejected, err := svc.RejectGroupInvite(ctx, group.GroupID, inviteB.InviteID, now.Add(7*time.Minute))
	if err != nil {
		t.Fatalf("RejectGroupInvite() error = %v", err)
	}
	if rejected.Status != GroupInviteStatusRejected {
		t.Fatalf("rejected status mismatch: got %s want %s", rejected.Status, GroupInviteStatusRejected)
	}

	details, err := svc.GetGroupDetails(ctx, group.GroupID)
	if err != nil {
		t.Fatalf("GetGroupDetails() error = %v", err)
	}
	if details.Group.Epoch != 2 {
		t.Fatalf("group epoch should not change on reject: got %d want 2", details.Group.Epoch)
	}
	if len(details.Members) != 2 {
		t.Fatalf("members length mismatch after accept+reject: got %d want 2", len(details.Members))
	}
}

func TestServiceGroupRoleAndRemoveConstraints(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 2, 24, 11, 0, 0, 0, time.UTC)
	store := NewFileStore(t.TempDir())
	svc := NewService(store)

	local, _, err := svc.EnsureIdentity(ctx, now)
	if err != nil {
		t.Fatalf("EnsureIdentity() error = %v", err)
	}
	group, err := svc.CreateGroup(ctx, now)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	remote, err := GenerateIdentity(now.Add(time.Minute))
	if err != nil {
		t.Fatalf("GenerateIdentity(remote) error = %v", err)
	}
	invite, err := svc.InviteGroupMember(ctx, group.GroupID, remote.PeerID, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("InviteGroupMember() error = %v", err)
	}
	if _, _, err := svc.AcceptGroupInvite(ctx, group.GroupID, invite.InviteID, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("AcceptGroupInvite() error = %v", err)
	}

	_, _, err = svc.SetGroupRole(ctx, group.GroupID, local.PeerID, GroupRoleMember, now.Add(4*time.Minute))
	if err == nil {
		t.Fatalf("expected demoting last manager to fail")
	}
	if !strings.Contains(err.Error(), "last manager") {
		t.Fatalf("unexpected demote-last-manager error: %v", err)
	}

	roleState, updatedGroup, err := svc.SetGroupRole(ctx, group.GroupID, remote.PeerID, GroupRoleManager, now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("SetGroupRole(promote remote manager) error = %v", err)
	}
	if roleState.RoleVersion <= 1 {
		t.Fatalf("role_version should increment after role update, got %d", roleState.RoleVersion)
	}
	if len(updatedGroup.ManagerPeerIDs) != 2 {
		t.Fatalf("manager_peer_ids length mismatch: got %d want 2", len(updatedGroup.ManagerPeerIDs))
	}

	_, err = svc.RemoveGroupMember(ctx, group.GroupID, remote.PeerID, now.Add(6*time.Minute))
	if err != nil {
		t.Fatalf("RemoveGroupMember(remote manager with fallback manager) error = %v", err)
	}
	_, err = svc.RemoveGroupMember(ctx, group.GroupID, local.PeerID, now.Add(7*time.Minute))
	if err == nil {
		t.Fatalf("expected removing last manager to fail")
	}
	if !strings.Contains(err.Error(), "last manager") {
		t.Fatalf("unexpected remove-last-manager error: %v", err)
	}
}

func TestServiceGroupInviteExpiryAndMaxMembers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 2, 24, 12, 0, 0, 0, time.UTC)
	store := NewFileStore(t.TempDir())
	svc := NewService(store)

	_, _, err := svc.EnsureIdentity(ctx, now)
	if err != nil {
		t.Fatalf("EnsureIdentity() error = %v", err)
	}
	group, err := svc.CreateGroup(ctx, now)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}

	group.MaxMembers = 1
	group.UpdatedAt = now.Add(time.Minute)
	if err := store.PutGroup(ctx, group); err != nil {
		t.Fatalf("PutGroup(max_members=1) error = %v", err)
	}

	peerA, err := GenerateIdentity(now.Add(2 * time.Minute))
	if err != nil {
		t.Fatalf("GenerateIdentity(peerA) error = %v", err)
	}
	_, err = svc.InviteGroupMember(ctx, group.GroupID, peerA.PeerID, now.Add(3*time.Minute))
	if err == nil {
		t.Fatalf("expected invite to fail when max_members reached")
	}
	if !strings.Contains(err.Error(), "limit reached") {
		t.Fatalf("unexpected max_members error: %v", err)
	}

	group.MaxMembers = GroupMaxMembers
	group.UpdatedAt = now.Add(4 * time.Minute)
	if err := store.PutGroup(ctx, group); err != nil {
		t.Fatalf("PutGroup(reset max_members) error = %v", err)
	}
	invite, err := svc.InviteGroupMember(ctx, group.GroupID, peerA.PeerID, now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("InviteGroupMember() error = %v", err)
	}

	invites, err := store.GetGroupInvites(ctx, group.GroupID)
	if err != nil {
		t.Fatalf("GetGroupInvites() error = %v", err)
	}
	for i := range invites {
		if invites[i].InviteID != invite.InviteID {
			continue
		}
		invites[i].ExpiresAt = now.Add(-(GroupInviteClockSkew + time.Second))
	}
	if err := store.PutGroupInvites(ctx, group.GroupID, invites); err != nil {
		t.Fatalf("PutGroupInvites(expired) error = %v", err)
	}

	_, _, err = svc.AcceptGroupInvite(ctx, group.GroupID, invite.InviteID, now)
	if err == nil {
		t.Fatalf("expected accepting expired invite to fail")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("unexpected expired invite error: %v", err)
	}

	details, err := svc.GetGroupDetails(ctx, group.GroupID)
	if err != nil {
		t.Fatalf("GetGroupDetails() error = %v", err)
	}
	foundExpired := false
	for _, item := range details.Invites {
		if item.InviteID == invite.InviteID && item.Status == GroupInviteStatusExpired {
			foundExpired = true
			break
		}
	}
	if !foundExpired {
		t.Fatalf("expected invite %s status to become expired", invite.InviteID)
	}
}
