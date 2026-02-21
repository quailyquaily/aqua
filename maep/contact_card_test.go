package maep

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBuildAndVerifyContactCard(t *testing.T) {
	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)
	identity, err := GenerateIdentity(now)
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}

	addr := fmt.Sprintf("/dns4/example.com/udp/4001/quic-v1/p2p/%s", identity.PeerID)
	card, err := BuildSignedContactCard(identity, []string{addr}, 1, 1, now, nil)
	if err != nil {
		t.Fatalf("BuildSignedContactCard() error = %v", err)
	}

	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("json.Marshal(card) error = %v", err)
	}

	parsed, err := ParseAndVerifyContactCard(raw, now)
	if err != nil {
		t.Fatalf("ParseAndVerifyContactCard() error = %v", err)
	}
	if parsed.Card.Payload.PeerID != identity.PeerID {
		t.Fatalf("peer_id mismatch: got %s want %s", parsed.Card.Payload.PeerID, identity.PeerID)
	}
}

func TestVerifyContactCardDetectsTamper(t *testing.T) {
	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)
	identity, err := GenerateIdentity(now)
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}

	addr := fmt.Sprintf("/dns4/example.com/udp/4001/quic-v1/p2p/%s", identity.PeerID)
	card, err := BuildSignedContactCard(identity, []string{addr}, 1, 1, now, nil)
	if err != nil {
		t.Fatalf("BuildSignedContactCard() error = %v", err)
	}

	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("json.Marshal(card) error = %v", err)
	}

	var tampered map[string]any
	if err := json.Unmarshal(raw, &tampered); err != nil {
		t.Fatalf("json.Unmarshal(raw) error = %v", err)
	}
	payload, ok := tampered["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload type assertion failed")
	}
	payload["node_uuid"] = "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f999"
	tamperedRaw, err := json.Marshal(tampered)
	if err != nil {
		t.Fatalf("json.Marshal(tampered) error = %v", err)
	}

	if _, err := ParseAndVerifyContactCard(tamperedRaw, now); err == nil {
		t.Fatalf("expected signature verification error, got nil")
	}
}

func TestBuildSignedContactCard_RejectsNonDialableAddress(t *testing.T) {
	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)
	identity, err := GenerateIdentity(now)
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}

	addr := fmt.Sprintf("/ip4/0.0.0.0/tcp/4021/p2p/%s", identity.PeerID)
	_, err = BuildSignedContactCard(identity, []string{addr}, 1, 1, now, nil)
	if err == nil {
		t.Fatalf("expected non-dialable address error, got nil")
	}
	if !strings.Contains(err.Error(), "non-dialable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseAndVerifyContactCard_RejectsNonDialableAddress(t *testing.T) {
	now := time.Date(2026, 2, 6, 12, 0, 0, 0, time.UTC)
	identity, err := GenerateIdentity(now)
	if err != nil {
		t.Fatalf("GenerateIdentity() error = %v", err)
	}

	payload := ContactCardPayload{
		Version:              ContactCardVersionV1,
		NodeUUID:             identity.NodeUUID,
		PeerID:               identity.PeerID,
		NodeID:               NodeIDFromPeerID(identity.PeerID),
		IdentityPubEd25519:   identity.IdentityPubEd25519,
		Addresses:            []string{fmt.Sprintf("/ip4/0.0.0.0/tcp/4021/p2p/%s", identity.PeerID)},
		MinSupportedProtocol: 1,
		MaxSupportedProtocol: 1,
		IssuedAt:             now,
	}
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(payload) error = %v", err)
	}
	canonicalPayload, err := canonicalizeJCS(payloadRaw)
	if err != nil {
		t.Fatalf("canonicalizeJCS(payload) error = %v", err)
	}
	priv, err := ParseIdentityPrivateKey(identity.IdentityPrivEd25519)
	if err != nil {
		t.Fatalf("ParseIdentityPrivateKey() error = %v", err)
	}
	sig, err := priv.Sign(buildContactCardSignInput(canonicalPayload))
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	rawCard, err := json.Marshal(ContactCardEnvelope{
		Payload:   payloadRaw,
		SigAlg:    ContactCardSigAlgEd25519,
		SigFormat: ContactCardSigFormatJCS,
		Sig:       encodeBase64URL(sig),
	})
	if err != nil {
		t.Fatalf("json.Marshal(card) error = %v", err)
	}

	_, err = ParseAndVerifyContactCard(rawCard, now)
	if err == nil {
		t.Fatalf("expected non-dialable address error, got nil")
	}
	if !strings.Contains(err.Error(), "non-dialable") {
		t.Fatalf("unexpected error: %v", err)
	}
}
