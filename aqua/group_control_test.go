package aqua

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestServiceApplyInboundGroupInviteStoresPendingInvite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)

	managerStore := NewFileStore(t.TempDir())
	managerSvc := NewService(managerStore)
	managerIdentity, _, err := managerSvc.EnsureIdentity(ctx, now)
	if err != nil {
		t.Fatalf("EnsureIdentity(manager) error = %v", err)
	}
	group, err := managerSvc.CreateGroup(ctx, now.Add(time.Second))
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}

	inviteeStore := NewFileStore(t.TempDir())
	inviteeSvc := NewService(inviteeStore)
	inviteeIdentity, _, err := inviteeSvc.EnsureIdentity(ctx, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("EnsureIdentity(invitee) error = %v", err)
	}

	invite, err := managerSvc.InviteGroupMember(ctx, group.GroupID, inviteeIdentity.PeerID, now.Add(3*time.Second))
	if err != nil {
		t.Fatalf("InviteGroupMember() error = %v", err)
	}
	payloadRaw, err := EncodeGroupInviteControlMessage(invite)
	if err != nil {
		t.Fatalf("EncodeGroupInviteControlMessage() error = %v", err)
	}

	if err := inviteeSvc.ApplyInboundGroupControl(ctx, managerIdentity.PeerID, payloadRaw, now.Add(4*time.Second)); err != nil {
		t.Fatalf("ApplyInboundGroupControl(invite) error = %v", err)
	}

	invites, err := inviteeSvc.ListGroupInvites(ctx, group.GroupID)
	if err != nil {
		t.Fatalf("ListGroupInvites(invitee) error = %v", err)
	}
	if len(invites) != 1 {
		t.Fatalf("invitee pending invite count mismatch: got %d want 1", len(invites))
	}
	if invites[0].InviteID != invite.InviteID {
		t.Fatalf("invite id mismatch: got %q want %q", invites[0].InviteID, invite.InviteID)
	}
	if invites[0].Status != GroupInviteStatusPending {
		t.Fatalf("invite status mismatch: got %s want %s", invites[0].Status, GroupInviteStatusPending)
	}
	if _, err := inviteeSvc.GetGroupDetails(ctx, group.GroupID); err == nil {
		t.Fatalf("invite-only receive should not create group state before accept")
	}
}

func TestServiceApplyInboundGroupInviteRejectDecision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC)

	managerStore := NewFileStore(t.TempDir())
	managerSvc := NewService(managerStore)
	managerIdentity, _, err := managerSvc.EnsureIdentity(ctx, now)
	if err != nil {
		t.Fatalf("EnsureIdentity(manager) error = %v", err)
	}
	group, err := managerSvc.CreateGroup(ctx, now.Add(time.Second))
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}

	inviteeIdentity, err := GenerateIdentity(now.Add(2 * time.Second))
	if err != nil {
		t.Fatalf("GenerateIdentity(invitee) error = %v", err)
	}
	invite, err := managerSvc.InviteGroupMember(ctx, group.GroupID, inviteeIdentity.PeerID, now.Add(3*time.Second))
	if err != nil {
		t.Fatalf("InviteGroupMember() error = %v", err)
	}

	decisionRaw, err := EncodeGroupInviteDecisionMessage(invite, GroupControlActionInviteReject, now.Add(4*time.Second))
	if err != nil {
		t.Fatalf("EncodeGroupInviteDecisionMessage(reject) error = %v", err)
	}
	if err := managerSvc.ApplyInboundGroupControl(ctx, inviteeIdentity.PeerID, decisionRaw, now.Add(5*time.Second)); err != nil {
		t.Fatalf("ApplyInboundGroupControl(reject) error = %v", err)
	}

	details, err := managerSvc.GetGroupDetails(ctx, group.GroupID)
	if err != nil {
		t.Fatalf("GetGroupDetails() error = %v", err)
	}
	if details.Group.Epoch != 1 {
		t.Fatalf("reject should not advance epoch: got %d want 1", details.Group.Epoch)
	}
	if len(details.Invites) != 1 || details.Invites[0].Status != GroupInviteStatusRejected {
		t.Fatalf("expected invite status rejected, got %+v", details.Invites)
	}
	if details.Group.LocalRole != GroupRoleManager {
		t.Fatalf("manager local role mismatch: got %s want %s", details.Group.LocalRole, GroupRoleManager)
	}
	if details.Group.ManagerPeerIDs[0] != managerIdentity.PeerID {
		t.Fatalf("manager peer id mismatch: got %v want [%s]", details.Group.ManagerPeerIDs, managerIdentity.PeerID)
	}
}

func TestNodeGroupInviteAcceptFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	now := time.Date(2026, 3, 10, 11, 0, 0, 0, time.UTC)

	managerSvc := NewService(NewFileStore(t.TempDir()))
	managerIdentity, _, err := managerSvc.EnsureIdentity(ctx, now)
	if err != nil {
		t.Fatalf("EnsureIdentity(manager) error = %v", err)
	}
	managerNode, err := NewNode(ctx, managerSvc, NodeOptions{
		ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"},
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
			t.Skipf("network listen unavailable in this environment: %v", err)
		}
		t.Fatalf("NewNode(manager) error = %v", err)
	}
	defer managerNode.Close()

	inviteeSvc := NewService(NewFileStore(t.TempDir()))
	inviteeIdentity, _, err := inviteeSvc.EnsureIdentity(ctx, now.Add(time.Second))
	if err != nil {
		t.Fatalf("EnsureIdentity(invitee) error = %v", err)
	}
	inviteeNode, err := NewNode(ctx, inviteeSvc, NodeOptions{
		ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"},
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
			t.Skipf("network listen unavailable in this environment: %v", err)
		}
		t.Fatalf("NewNode(invitee) error = %v", err)
	}
	defer inviteeNode.Close()

	if err := importContactForNodeTest(ctx, managerSvc, inviteeSvc, preferredDialAddresses(managerNode.AddrStrings()), now.Add(2*time.Second)); err != nil {
		t.Fatalf("import manager->invitee contact error = %v", err)
	}
	if err := importContactForNodeTest(ctx, inviteeSvc, managerSvc, preferredDialAddresses(inviteeNode.AddrStrings()), now.Add(3*time.Second)); err != nil {
		t.Fatalf("import invitee->manager contact error = %v", err)
	}

	group, err := managerSvc.CreateGroup(ctx, now.Add(4*time.Second))
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	invite, err := managerSvc.InviteGroupMember(ctx, group.GroupID, inviteeIdentity.PeerID, now.Add(5*time.Second))
	if err != nil {
		t.Fatalf("InviteGroupMember() error = %v", err)
	}
	inviteRaw, err := EncodeGroupInviteControlMessage(invite)
	if err != nil {
		t.Fatalf("EncodeGroupInviteControlMessage() error = %v", err)
	}
	if _, err := managerNode.PushData(ctx, inviteeIdentity.PeerID, nil, BuildGroupControlPushRequest(inviteRaw, "test-invite:"+invite.InviteID), false); err != nil {
		t.Fatalf("PushData(invite) error = %v", err)
	}

	waitForNodeTest(t, 3*time.Second, func() error {
		invites, err := inviteeSvc.ListGroupInvites(ctx, group.GroupID)
		if err != nil {
			return err
		}
		if len(invites) != 1 {
			return fmt.Errorf("invite count = %d want 1", len(invites))
		}
		if invites[0].Status != GroupInviteStatusPending {
			return fmt.Errorf("invite status = %s want %s", invites[0].Status, GroupInviteStatusPending)
		}
		_, err = inviteeSvc.GetGroupDetails(ctx, group.GroupID)
		if err == nil {
			return fmt.Errorf("group state should not exist before accept")
		}
		return nil
	})

	acceptedInvite, acceptedGroup, err := inviteeSvc.AcceptGroupInvite(ctx, group.GroupID, invite.InviteID, now.Add(6*time.Second))
	if err != nil {
		t.Fatalf("AcceptGroupInvite(invitee) error = %v", err)
	}
	if acceptedGroup.LocalRole != GroupRoleMember {
		t.Fatalf("accepted local role mismatch: got %s want %s", acceptedGroup.LocalRole, GroupRoleMember)
	}
	decisionRaw, err := EncodeGroupInviteDecisionMessage(acceptedInvite, GroupControlActionInviteAccept, now.Add(7*time.Second))
	if err != nil {
		t.Fatalf("EncodeGroupInviteDecisionMessage(accept) error = %v", err)
	}
	if _, err := inviteeNode.PushData(ctx, managerIdentity.PeerID, nil, BuildGroupControlPushRequest(decisionRaw, "test-accept:"+invite.InviteID), false); err != nil {
		t.Fatalf("PushData(accept) error = %v", err)
	}

	waitForNodeTest(t, 3*time.Second, func() error {
		details, err := managerSvc.GetGroupDetails(ctx, group.GroupID)
		if err != nil {
			return err
		}
		if details.Group.Epoch != 2 {
			return fmt.Errorf("epoch = %d want 2", details.Group.Epoch)
		}
		if len(details.Members) != 2 {
			return fmt.Errorf("member count = %d want 2", len(details.Members))
		}
		for _, item := range details.Invites {
			if item.InviteID == invite.InviteID && item.Status == GroupInviteStatusAccepted {
				return nil
			}
		}
		return fmt.Errorf("invite %s not accepted yet", invite.InviteID)
	})
}

func importContactForNodeTest(ctx context.Context, fromSvc *Service, toSvc *Service, addresses []string, now time.Time) error {
	_, rawCard, err := fromSvc.ExportContactCard(ctx, addresses, ProtocolVersionV1, ProtocolVersionV1, now, nil)
	if err != nil {
		return err
	}
	_, err = toSvc.ImportContactCard(ctx, rawCard, "", now)
	return err
}

func preferredDialAddresses(addresses []string) []string {
	preferred := make([]string, 0, len(addresses))
	for _, addr := range addresses {
		if strings.Contains(addr, "/ip4/127.0.0.1/") {
			preferred = append(preferred, addr)
		}
	}
	if len(preferred) > 0 {
		return preferred
	}
	return append([]string(nil), addresses...)
}

func waitForNodeTest(t *testing.T, timeout time.Duration, fn func() error) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		lastErr = fn()
		if lastErr == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s: %v", timeout, lastErr)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
