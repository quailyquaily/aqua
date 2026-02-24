package main

import (
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

	stdoutInvite, stderrInvite, err := executeCLI(t, "--dir", dir, "group", "invite", created.GroupID, remote.PeerID, "--json")
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

	if _, stderrAccept, err := executeCLI(t, "--dir", dir, "group", "invite", "accept", created.GroupID, invite.InviteID, "--json"); err != nil {
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
