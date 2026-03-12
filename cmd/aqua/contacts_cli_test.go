package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/quailyquaily/aqua/aqua"
)

func TestContactsCLI_AddLocalOnlyJSON(t *testing.T) {
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
		t.Fatalf("expected local identity after init")
	}

	remoteIdentity, err := aqua.GenerateIdentity(time.Date(2026, 3, 12, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(remote) error = %v", err)
	}

	localAddr := fmt.Sprintf("/ip4/127.0.0.1/tcp/4111/p2p/%s", localIdentity.PeerID)
	remoteAddr := fmt.Sprintf("/ip4/127.0.0.1/tcp/4112/p2p/%s", remoteIdentity.PeerID)

	stdout, stderr, err := executeCLI(
		t,
		"--dir", dir,
		"contacts", "invite", remoteAddr,
		"--address", localAddr,
		"--display-name", "remote-alias",
		"--verify",
		"--local-only",
		"--json",
	)
	if err != nil {
		t.Fatalf("contacts invite --local-only --json error = %v, stderr=%s", err, stderr)
	}

	var view contactRequestView
	if err := json.Unmarshal([]byte(stdout), &view); err != nil {
		t.Fatalf("decode contacts invite json error = %v, stdout=%s", err, stdout)
	}
	if view.Direction != "outgoing" {
		t.Fatalf("request direction mismatch: got %q want %q", view.Direction, "outgoing")
	}
	if view.PeerID != remoteIdentity.PeerID {
		t.Fatalf("request peer_id mismatch: got %q want %q", view.PeerID, remoteIdentity.PeerID)
	}
	if view.Status != aqua.ContactRequestStatusPending {
		t.Fatalf("request status mismatch: got %s want %s", view.Status, aqua.ContactRequestStatusPending)
	}
	if !view.VerifyOnAccept {
		t.Fatalf("verify_on_accept mismatch: got false want true")
	}

	requests, err := localSvc.ListContactRequests(ctx)
	if err != nil {
		t.Fatalf("ListContactRequests(local) error = %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("request count mismatch: got %d want 1", len(requests))
	}
	if requests[0].DisplayName != "remote-alias" {
		t.Fatalf("stored display_name mismatch: got %q want %q", requests[0].DisplayName, "remote-alias")
	}
}

func TestContactsCLI_RequestsAndAcceptJSON(t *testing.T) {
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
		t.Fatalf("expected local identity after init")
	}

	remoteSvc := aqua.NewService(aqua.NewFileStore(t.TempDir()))
	remoteIdentity, _, err := remoteSvc.EnsureIdentity(ctx, time.Date(2026, 3, 12, 17, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("EnsureIdentity(remote) error = %v", err)
	}
	_, remoteCardRaw, err := remoteSvc.ExportContactCard(
		ctx,
		[]string{fmt.Sprintf("/ip4/127.0.0.1/tcp/4113/p2p/%s", remoteIdentity.PeerID)},
		aqua.ProtocolVersionV1,
		aqua.ProtocolVersionV1,
		time.Date(2026, 3, 12, 17, 1, 0, 0, time.UTC),
		nil,
	)
	if err != nil {
		t.Fatalf("ExportContactCard(remote) error = %v", err)
	}

	request, err := remoteSvc.CreateContactRequest(ctx, localIdentity.PeerID, remoteCardRaw, "", true, time.Date(2026, 3, 12, 17, 2, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateContactRequest(remote) error = %v", err)
	}
	payloadRaw, err := aqua.EncodeContactRequestControlMessage(request)
	if err != nil {
		t.Fatalf("EncodeContactRequestControlMessage() error = %v", err)
	}
	if err := localSvc.ApplyInboundContactControl(ctx, remoteIdentity.PeerID, payloadRaw, time.Date(2026, 3, 12, 17, 3, 0, 0, time.UTC)); err != nil {
		t.Fatalf("ApplyInboundContactControl(local) error = %v", err)
	}

	stdoutRequests, stderrRequests, err := executeCLI(t, "--dir", dir, "contacts", "invites", "--json")
	if err != nil {
		t.Fatalf("contacts invites --json error = %v, stderr=%s", err, stderrRequests)
	}
	var listed []contactRequestView
	if err := json.Unmarshal([]byte(stdoutRequests), &listed); err != nil {
		t.Fatalf("decode contacts invites json error = %v, stdout=%s", err, stdoutRequests)
	}
	if len(listed) != 1 {
		t.Fatalf("incoming request count mismatch: got %d want 1", len(listed))
	}
	if listed[0].Direction != "incoming" {
		t.Fatalf("incoming direction mismatch: got %q want %q", listed[0].Direction, "incoming")
	}
	if listed[0].PeerID != remoteIdentity.PeerID {
		t.Fatalf("incoming peer_id mismatch: got %q want %q", listed[0].PeerID, remoteIdentity.PeerID)
	}

	stdoutAccept, stderrAccept, err := executeCLI(
		t,
		"--dir", dir,
		"contacts", "invite", "accept", remoteIdentity.PeerID,
		"--display-name", "trusted-remote",
		"--local-only",
		"--json",
	)
	if err != nil {
		t.Fatalf("contacts add accept --json error = %v, stderr=%s", err, stderrAccept)
	}
	var accepted struct {
		Request contactRequestView `json:"request"`
		Contact struct {
			PeerID     string          `json:"peer_id"`
			TrustState aqua.TrustState `json:"trust_state"`
		} `json:"contact"`
	}
	if err := json.Unmarshal([]byte(stdoutAccept), &accepted); err != nil {
		t.Fatalf("decode contacts add accept json error = %v, stdout=%s", err, stdoutAccept)
	}
	if accepted.Request.Status != aqua.ContactRequestStatusAccepted {
		t.Fatalf("accepted request status mismatch: got %s want %s", accepted.Request.Status, aqua.ContactRequestStatusAccepted)
	}
	if accepted.Contact.PeerID != remoteIdentity.PeerID {
		t.Fatalf("accepted contact peer_id mismatch: got %q want %q", accepted.Contact.PeerID, remoteIdentity.PeerID)
	}
	if accepted.Contact.TrustState != aqua.TrustStateVerified {
		t.Fatalf("accepted contact trust_state mismatch: got %s want %s", accepted.Contact.TrustState, aqua.TrustStateVerified)
	}

	contact, ok, err := localSvc.GetContactByPeerID(ctx, remoteIdentity.PeerID)
	if err != nil {
		t.Fatalf("GetContactByPeerID(local) error = %v", err)
	}
	if !ok {
		t.Fatalf("accepted contact missing from local store")
	}
	if contact.DisplayName != "trusted-remote" {
		t.Fatalf("accepted contact display_name mismatch: got %q want %q", contact.DisplayName, "trusted-remote")
	}
	if contact.TrustState != aqua.TrustStateVerified {
		t.Fatalf("accepted contact trust_state mismatch: got %s want %s", contact.TrustState, aqua.TrustStateVerified)
	}
}
