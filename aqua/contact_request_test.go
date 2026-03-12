package aqua

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestServiceApplyInboundContactRequestStoresPendingRequest(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 3, 12, 13, 0, 0, 0, time.UTC)

	requesterSvc := NewService(NewFileStore(t.TempDir()))
	requesterIdentity, _, err := requesterSvc.EnsureIdentity(ctx, now)
	if err != nil {
		t.Fatalf("EnsureIdentity(requester) error = %v", err)
	}
	requesterCardRaw := exportContactRequestCardRawForTest(t, requesterSvc, requesterIdentity.PeerID, 4101, now.Add(time.Second))

	recipientSvc := NewService(NewFileStore(t.TempDir()))
	recipientIdentity, _, err := recipientSvc.EnsureIdentity(ctx, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("EnsureIdentity(recipient) error = %v", err)
	}

	request, err := requesterSvc.CreateContactRequest(ctx, recipientIdentity.PeerID, requesterCardRaw, "remote-bob", true, now.Add(3*time.Second))
	if err != nil {
		t.Fatalf("CreateContactRequest() error = %v", err)
	}
	payloadRaw, err := EncodeContactRequestControlMessage(request)
	if err != nil {
		t.Fatalf("EncodeContactRequestControlMessage() error = %v", err)
	}

	if err := recipientSvc.ApplyInboundContactControl(ctx, requesterIdentity.PeerID, payloadRaw, now.Add(4*time.Second)); err != nil {
		t.Fatalf("ApplyInboundContactControl(request) error = %v", err)
	}

	requests, err := recipientSvc.ListContactRequests(ctx)
	if err != nil {
		t.Fatalf("ListContactRequests(recipient) error = %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("request count mismatch: got %d want 1", len(requests))
	}
	if requests[0].RequestID != request.RequestID {
		t.Fatalf("request_id mismatch: got %q want %q", requests[0].RequestID, request.RequestID)
	}
	if requests[0].Status != ContactRequestStatusPending {
		t.Fatalf("request status mismatch: got %s want %s", requests[0].Status, ContactRequestStatusPending)
	}
	if !requests[0].VerifyOnAccept {
		t.Fatalf("verify_on_accept mismatch: got false want true")
	}
	if requests[0].DisplayName != "" {
		t.Fatalf("incoming request should not carry initiator display_name, got %q", requests[0].DisplayName)
	}
	if _, ok, err := recipientSvc.GetContactByPeerID(ctx, requesterIdentity.PeerID); err != nil {
		t.Fatalf("GetContactByPeerID(recipient) error = %v", err)
	} else if ok {
		t.Fatalf("recipient should not have contact before accept")
	}
}

func TestServiceContactRequestAcceptFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 3, 12, 14, 0, 0, 0, time.UTC)

	requesterSvc := NewService(NewFileStore(t.TempDir()))
	requesterIdentity, _, err := requesterSvc.EnsureIdentity(ctx, now)
	if err != nil {
		t.Fatalf("EnsureIdentity(requester) error = %v", err)
	}
	requesterCardRaw := exportContactRequestCardRawForTest(t, requesterSvc, requesterIdentity.PeerID, 4102, now.Add(time.Second))

	recipientSvc := NewService(NewFileStore(t.TempDir()))
	recipientIdentity, _, err := recipientSvc.EnsureIdentity(ctx, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("EnsureIdentity(recipient) error = %v", err)
	}
	recipientCardRaw := exportContactRequestCardRawForTest(t, recipientSvc, recipientIdentity.PeerID, 4103, now.Add(3*time.Second))

	request, err := requesterSvc.CreateContactRequest(ctx, recipientIdentity.PeerID, requesterCardRaw, "preferred-remote", true, now.Add(4*time.Second))
	if err != nil {
		t.Fatalf("CreateContactRequest() error = %v", err)
	}
	requestPayloadRaw, err := EncodeContactRequestControlMessage(request)
	if err != nil {
		t.Fatalf("EncodeContactRequestControlMessage() error = %v", err)
	}
	if err := recipientSvc.ApplyInboundContactControl(ctx, requesterIdentity.PeerID, requestPayloadRaw, now.Add(5*time.Second)); err != nil {
		t.Fatalf("ApplyInboundContactControl(request) error = %v", err)
	}

	accepted, recipientContact, err := recipientSvc.AcceptContactRequest(ctx, requesterIdentity.PeerID, "trusted-alice", now.Add(6*time.Second))
	if err != nil {
		t.Fatalf("AcceptContactRequest() error = %v", err)
	}
	if accepted.Status != ContactRequestStatusAccepted {
		t.Fatalf("accepted request status mismatch: got %s want %s", accepted.Status, ContactRequestStatusAccepted)
	}
	if recipientContact.PeerID != requesterIdentity.PeerID {
		t.Fatalf("recipient contact peer mismatch: got %q want %q", recipientContact.PeerID, requesterIdentity.PeerID)
	}
	if recipientContact.DisplayName != "trusted-alice" {
		t.Fatalf("recipient contact display_name mismatch: got %q want %q", recipientContact.DisplayName, "trusted-alice")
	}
	if recipientContact.TrustState != TrustStateVerified {
		t.Fatalf("recipient contact trust_state mismatch: got %s want %s", recipientContact.TrustState, TrustStateVerified)
	}

	decisionRaw, err := EncodeContactDecisionControlMessage(accepted, ContactControlActionAccept, recipientCardRaw, now.Add(7*time.Second))
	if err != nil {
		t.Fatalf("EncodeContactDecisionControlMessage(accept) error = %v", err)
	}
	if err := requesterSvc.ApplyInboundContactControl(ctx, recipientIdentity.PeerID, decisionRaw, now.Add(8*time.Second)); err != nil {
		t.Fatalf("ApplyInboundContactControl(accept) error = %v", err)
	}

	requesterContact, ok, err := requesterSvc.GetContactByPeerID(ctx, recipientIdentity.PeerID)
	if err != nil {
		t.Fatalf("GetContactByPeerID(requester) error = %v", err)
	}
	if !ok {
		t.Fatalf("requester contact missing after accept decision")
	}
	if requesterContact.DisplayName != "preferred-remote" {
		t.Fatalf("requester contact display_name mismatch: got %q want %q", requesterContact.DisplayName, "preferred-remote")
	}
	if requesterContact.TrustState != TrustStateVerified {
		t.Fatalf("requester contact trust_state mismatch: got %s want %s", requesterContact.TrustState, TrustStateVerified)
	}

	requests, err := requesterSvc.ListContactRequests(ctx)
	if err != nil {
		t.Fatalf("ListContactRequests(requester) error = %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("requester request count mismatch: got %d want 1", len(requests))
	}
	if requests[0].Status != ContactRequestStatusAccepted {
		t.Fatalf("requester request status mismatch: got %s want %s", requests[0].Status, ContactRequestStatusAccepted)
	}
	if requests[0].RespondedAt == nil {
		t.Fatalf("requester request responded_at should be set after accept")
	}
}

func TestNodeContactRequestAcceptFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	now := time.Date(2026, 3, 12, 15, 0, 0, 0, time.UTC)

	requesterSvc := NewService(NewFileStore(t.TempDir()))
	requesterIdentity, _, err := requesterSvc.EnsureIdentity(ctx, now)
	if err != nil {
		t.Fatalf("EnsureIdentity(requester) error = %v", err)
	}
	requesterNode, err := NewNode(ctx, requesterSvc, NodeOptions{
		ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"},
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
			t.Skipf("network listen unavailable in this environment: %v", err)
		}
		t.Fatalf("NewNode(requester) error = %v", err)
	}
	defer requesterNode.Close()

	recipientSvc := NewService(NewFileStore(t.TempDir()))
	recipientIdentity, _, err := recipientSvc.EnsureIdentity(ctx, now.Add(time.Second))
	if err != nil {
		t.Fatalf("EnsureIdentity(recipient) error = %v", err)
	}
	recipientNode, err := NewNode(ctx, recipientSvc, NodeOptions{
		ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"},
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
			t.Skipf("network listen unavailable in this environment: %v", err)
		}
		t.Fatalf("NewNode(recipient) error = %v", err)
	}
	defer recipientNode.Close()

	_, requesterCardRaw, err := requesterSvc.ExportContactCard(ctx, preferredDialAddresses(requesterNode.AddrStrings()), ProtocolVersionV1, ProtocolVersionV1, now.Add(2*time.Second), nil)
	if err != nil {
		t.Fatalf("ExportContactCard(requester) error = %v", err)
	}
	requesterRequest, err := requesterSvc.CreateContactRequest(ctx, recipientIdentity.PeerID, requesterCardRaw, "recipient-via-rpc", true, now.Add(3*time.Second))
	if err != nil {
		t.Fatalf("CreateContactRequest() error = %v", err)
	}
	requestPayloadRaw, err := EncodeContactRequestControlMessage(requesterRequest)
	if err != nil {
		t.Fatalf("EncodeContactRequestControlMessage() error = %v", err)
	}
	if err := requesterNode.PushContactControl(ctx, recipientIdentity.PeerID, preferredDialAddresses(recipientNode.AddrStrings()), requestPayloadRaw); err != nil {
		t.Fatalf("PushContactControl(request) error = %v", err)
	}

	waitForNodeTest(t, 3*time.Second, func() error {
		requests, err := recipientSvc.ListContactRequests(ctx)
		if err != nil {
			return err
		}
		if len(requests) != 1 {
			return fmt.Errorf("recipient request count = %d want 1", len(requests))
		}
		if requests[0].Status != ContactRequestStatusPending {
			return fmt.Errorf("recipient request status = %s want %s", requests[0].Status, ContactRequestStatusPending)
		}
		return nil
	})

	accepted, recipientContact, err := recipientSvc.AcceptContactRequest(ctx, requesterIdentity.PeerID, "requester-via-rpc", now.Add(4*time.Second))
	if err != nil {
		t.Fatalf("AcceptContactRequest() error = %v", err)
	}
	if recipientContact.TrustState != TrustStateVerified {
		t.Fatalf("recipient contact trust_state mismatch: got %s want %s", recipientContact.TrustState, TrustStateVerified)
	}

	_, recipientCardRaw, err := recipientSvc.ExportContactCard(ctx, preferredDialAddresses(recipientNode.AddrStrings()), ProtocolVersionV1, ProtocolVersionV1, now.Add(5*time.Second), nil)
	if err != nil {
		t.Fatalf("ExportContactCard(recipient) error = %v", err)
	}
	decisionRaw, err := EncodeContactDecisionControlMessage(accepted, ContactControlActionAccept, recipientCardRaw, now.Add(6*time.Second))
	if err != nil {
		t.Fatalf("EncodeContactDecisionControlMessage(accept) error = %v", err)
	}
	if err := recipientNode.PushContactControl(ctx, requesterIdentity.PeerID, preferredDialAddresses(requesterNode.AddrStrings()), decisionRaw); err != nil {
		t.Fatalf("PushContactControl(accept) error = %v", err)
	}

	waitForNodeTest(t, 3*time.Second, func() error {
		contact, ok, err := requesterSvc.GetContactByPeerID(ctx, recipientIdentity.PeerID)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("requester contact missing")
		}
		if contact.TrustState != TrustStateVerified {
			return fmt.Errorf("requester contact trust_state = %s want %s", contact.TrustState, TrustStateVerified)
		}
		if contact.DisplayName != "recipient-via-rpc" {
			return fmt.Errorf("requester contact display_name = %q want %q", contact.DisplayName, "recipient-via-rpc")
		}
		return nil
	})
}

func exportContactRequestCardRawForTest(t *testing.T, svc *Service, peerID string, port int, now time.Time) []byte {
	t.Helper()

	_, rawCard, err := svc.ExportContactCard(
		context.Background(),
		[]string{fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/p2p/%s", port, peerID)},
		ProtocolVersionV1,
		ProtocolVersionV1,
		now,
		nil,
	)
	if err != nil {
		t.Fatalf("ExportContactCard() error = %v", err)
	}
	return rawCard
}
