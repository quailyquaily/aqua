package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/quailyquaily/aqua/aqua"
)

func TestGroupCLI_CreateListShowJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if _, stderr, err := executeCLI(t, "--dir", dir, "init", "--json"); err != nil {
		t.Fatalf("init --json error = %v, stderr=%s", err, stderr)
	}

	stdoutCreate, stderrCreate, err := executeCLI(t, "--dir", dir, "group", "create", "--json")
	if err != nil {
		t.Fatalf("group create --json error = %v, stderr=%s", err, stderrCreate)
	}
	var created aqua.Group
	if err := json.Unmarshal([]byte(stdoutCreate), &created); err != nil {
		t.Fatalf("decode group create json error = %v, stdout=%s", err, stdoutCreate)
	}
	if created.GroupID == "" {
		t.Fatalf("group create should return group_id")
	}

	stdoutList, stderrList, err := executeCLI(t, "--dir", dir, "group", "list", "--json")
	if err != nil {
		t.Fatalf("group list --json error = %v, stderr=%s", err, stderrList)
	}
	var listed []aqua.Group
	if err := json.Unmarshal([]byte(stdoutList), &listed); err != nil {
		t.Fatalf("decode group list json error = %v, stdout=%s", err, stdoutList)
	}
	if len(listed) != 1 {
		t.Fatalf("group list length mismatch: got %d want 1", len(listed))
	}
	if listed[0].GroupID != created.GroupID {
		t.Fatalf("group id mismatch: got %q want %q", listed[0].GroupID, created.GroupID)
	}

	stdoutShow, stderrShow, err := executeCLI(t, "--dir", dir, "group", "show", created.GroupID, "--json")
	if err != nil {
		t.Fatalf("group show --json error = %v, stderr=%s", err, stderrShow)
	}
	var details aqua.GroupDetails
	if err := json.Unmarshal([]byte(stdoutShow), &details); err != nil {
		t.Fatalf("decode group show json error = %v, stdout=%s", err, stdoutShow)
	}
	if details.Group.GroupID != created.GroupID {
		t.Fatalf("group show id mismatch: got %q want %q", details.Group.GroupID, created.GroupID)
	}
	if len(details.Members) != 1 {
		t.Fatalf("group show members mismatch: got %d want 1", len(details.Members))
	}
}

func TestGroupCLI_InviteAcceptRoleJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if _, stderr, err := executeCLI(t, "--dir", dir, "init", "--json"); err != nil {
		t.Fatalf("init --json error = %v, stderr=%s", err, stderr)
	}

	stdoutCreate, stderrCreate, err := executeCLI(t, "--dir", dir, "group", "create", "--json")
	if err != nil {
		t.Fatalf("group create --json error = %v, stderr=%s", err, stderrCreate)
	}
	var created aqua.Group
	if err := json.Unmarshal([]byte(stdoutCreate), &created); err != nil {
		t.Fatalf("decode group create json error = %v, stdout=%s", err, stdoutCreate)
	}

	remote, err := aqua.GenerateIdentity(time.Date(2026, 2, 24, 13, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(remote) error = %v", err)
	}

	stdoutInvite, stderrInvite, err := executeCLI(t, "--dir", dir, "group", "invite", created.GroupID, remote.PeerID, "--local-only", "--json")
	if err != nil {
		t.Fatalf("group invite --json error = %v, stderr=%s", err, stderrInvite)
	}
	var invite aqua.GroupInvite
	if err := json.Unmarshal([]byte(stdoutInvite), &invite); err != nil {
		t.Fatalf("decode group invite json error = %v, stdout=%s", err, stdoutInvite)
	}
	if invite.Status != aqua.GroupInviteStatusPending {
		t.Fatalf("invite status mismatch: got %s want %s", invite.Status, aqua.GroupInviteStatusPending)
	}

	if _, stderrAccept, err := executeCLI(t, "--dir", dir, "group", "invite", "accept", created.GroupID, invite.InviteID, "--local-only", "--json"); err != nil {
		t.Fatalf("group invite accept --json error = %v, stderr=%s", err, stderrAccept)
	}
	if _, stderrRole, err := executeCLI(t, "--dir", dir, "group", "role", created.GroupID, remote.PeerID, "manager", "--json"); err != nil {
		t.Fatalf("group role manager --json error = %v, stderr=%s", err, stderrRole)
	}

	stdoutShow, stderrShow, err := executeCLI(t, "--dir", dir, "group", "show", created.GroupID, "--json")
	if err != nil {
		t.Fatalf("group show --json error = %v, stderr=%s", err, stderrShow)
	}
	var details aqua.GroupDetails
	if err := json.Unmarshal([]byte(stdoutShow), &details); err != nil {
		t.Fatalf("decode group show json error = %v, stdout=%s", err, stdoutShow)
	}
	if len(details.Group.ManagerPeerIDs) != 2 {
		t.Fatalf("manager peer ids mismatch: got %d want 2", len(details.Group.ManagerPeerIDs))
	}
}

func TestGroupCLI_InvitesJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if _, stderr, err := executeCLI(t, "--dir", dir, "init", "--json"); err != nil {
		t.Fatalf("init --json error = %v, stderr=%s", err, stderr)
	}

	stdoutCreate, stderrCreate, err := executeCLI(t, "--dir", dir, "group", "create", "--json")
	if err != nil {
		t.Fatalf("group create --json error = %v, stderr=%s", err, stderrCreate)
	}
	var created aqua.Group
	if err := json.Unmarshal([]byte(stdoutCreate), &created); err != nil {
		t.Fatalf("decode group create json error = %v, stdout=%s", err, stdoutCreate)
	}

	remote, err := aqua.GenerateIdentity(time.Date(2026, 3, 10, 13, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(remote) error = %v", err)
	}
	stdoutInvite, stderrInvite, err := executeCLI(t, "--dir", dir, "group", "invite", created.GroupID, remote.PeerID, "--local-only", "--json")
	if err != nil {
		t.Fatalf("group invite --json error = %v, stderr=%s", err, stderrInvite)
	}
	var invite aqua.GroupInvite
	if err := json.Unmarshal([]byte(stdoutInvite), &invite); err != nil {
		t.Fatalf("decode group invite json error = %v, stdout=%s", err, stdoutInvite)
	}

	stdoutInvites, stderrInvites, err := executeCLI(t, "--dir", dir, "group", "invites", "--status", "pending", "--json")
	if err != nil {
		t.Fatalf("group invites --json error = %v, stderr=%s", err, stderrInvites)
	}
	var invites []aqua.GroupInvite
	if err := json.Unmarshal([]byte(stdoutInvites), &invites); err != nil {
		t.Fatalf("decode group invites json error = %v, stdout=%s", err, stdoutInvites)
	}
	if len(invites) != 1 {
		t.Fatalf("pending invite count mismatch: got %d want 1", len(invites))
	}
	if invites[0].InviteID != invite.InviteID {
		t.Fatalf("invite id mismatch: got %q want %q", invites[0].InviteID, invite.InviteID)
	}
}

func TestGroupCLI_InviteAcceptByGroupIDJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if _, stderr, err := executeCLI(t, "--dir", dir, "init", "--json"); err != nil {
		t.Fatalf("init --json error = %v, stderr=%s", err, stderr)
	}

	ctx := context.Background()
	localSvc := aqua.NewService(aqua.NewFileStore(dir))
	localIdentity, ok, err := localSvc.GetIdentity(ctx)
	if err != nil {
		t.Fatalf("GetIdentity(local) error = %v", err)
	}
	if !ok {
		t.Fatalf("expected local identity to exist after init")
	}

	remoteSvc := aqua.NewService(aqua.NewFileStore(t.TempDir()))
	remoteIdentity, _, err := remoteSvc.EnsureIdentity(ctx, time.Date(2026, 3, 10, 14, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("EnsureIdentity(remote) error = %v", err)
	}
	group, err := remoteSvc.CreateGroup(ctx, time.Date(2026, 3, 10, 14, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateGroup(remote) error = %v", err)
	}
	invite, err := remoteSvc.InviteGroupMember(ctx, group.GroupID, localIdentity.PeerID, time.Date(2026, 3, 10, 14, 2, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("InviteGroupMember(remote) error = %v", err)
	}
	payloadRaw, err := aqua.EncodeGroupInviteControlMessage(invite)
	if err != nil {
		t.Fatalf("EncodeGroupInviteControlMessage() error = %v", err)
	}
	if err := localSvc.ApplyInboundGroupControl(ctx, remoteIdentity.PeerID, payloadRaw, time.Date(2026, 3, 10, 14, 3, 0, 0, time.UTC)); err != nil {
		t.Fatalf("ApplyInboundGroupControl(local) error = %v", err)
	}

	stdoutAccept, stderrAccept, err := executeCLI(t, "--dir", dir, "group", "invite", "accept", group.GroupID, "--local-only", "--json")
	if err != nil {
		t.Fatalf("group invite accept <group_id> --json error = %v, stderr=%s", err, stderrAccept)
	}
	var accepted struct {
		Invite aqua.GroupInvite `json:"invite"`
		Group  aqua.Group       `json:"group"`
	}
	if err := json.Unmarshal([]byte(stdoutAccept), &accepted); err != nil {
		t.Fatalf("decode group invite accept json error = %v, stdout=%s", err, stdoutAccept)
	}
	if accepted.Invite.InviteID != invite.InviteID {
		t.Fatalf("accepted invite id mismatch: got %q want %q", accepted.Invite.InviteID, invite.InviteID)
	}
	if accepted.Invite.Status != aqua.GroupInviteStatusAccepted {
		t.Fatalf("accepted invite status mismatch: got %s want %s", accepted.Invite.Status, aqua.GroupInviteStatusAccepted)
	}
	if accepted.Group.LocalRole != aqua.GroupRoleMember {
		t.Fatalf("accepted local role mismatch: got %s want %s", accepted.Group.LocalRole, aqua.GroupRoleMember)
	}
}
