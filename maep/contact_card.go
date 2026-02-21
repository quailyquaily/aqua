package maep

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/google/uuid"
	ic "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

const ed25519PublicKeyBytes = 32

func BuildSignedContactCard(identity Identity, addresses []string, minProtocol int, maxProtocol int, issuedAt time.Time, expiresAt *time.Time) (ContactCard, error) {
	if minProtocol <= 0 || maxProtocol <= 0 {
		return ContactCard{}, WrapProtocolError(ErrInvalidParams, "protocol versions must be positive")
	}
	if minProtocol > maxProtocol {
		return ContactCard{}, WrapProtocolError(ErrInvalidParams, "min_supported_protocol cannot be greater than max_supported_protocol")
	}
	if issuedAt.IsZero() {
		issuedAt = time.Now().UTC()
	}
	issuedAt = issuedAt.UTC()

	if strings.TrimSpace(identity.NodeUUID) == "" || strings.TrimSpace(identity.PeerID) == "" || strings.TrimSpace(identity.IdentityPubEd25519) == "" {
		return ContactCard{}, WrapProtocolError(ErrInvalidParams, "identity is incomplete")
	}

	peerID, err := peer.Decode(identity.PeerID)
	if err != nil {
		return ContactCard{}, WrapProtocolError(ErrInvalidParams, "identity.peer_id is invalid: %v", err)
	}

	pubPeerID, err := DerivePeerIDFromIdentityPub(identity.IdentityPubEd25519)
	if err != nil {
		return ContactCard{}, err
	}
	if pubPeerID != peerID {
		return ContactCard{}, WrapProtocolError(ErrInvalidParams, "identity.peer_id does not match identity_pub_ed25519")
	}

	normalizedAddresses := normalizeAddresses(addresses)
	if len(normalizedAddresses) == 0 {
		return ContactCard{}, WrapProtocolError(ErrInvalidParams, "at least one address is required")
	}
	for _, addr := range normalizedAddresses {
		if err := validateAddressMatchesPeerID(addr, peerID); err != nil {
			return ContactCard{}, err
		}
	}

	payload := ContactCardPayload{
		Version:              ContactCardVersionV1,
		NodeUUID:             strings.TrimSpace(identity.NodeUUID),
		PeerID:               peerID.String(),
		NodeID:               NodeIDFromPeerID(peerID.String()),
		IdentityPubEd25519:   strings.TrimSpace(identity.IdentityPubEd25519),
		Addresses:            normalizedAddresses,
		MinSupportedProtocol: minProtocol,
		MaxSupportedProtocol: maxProtocol,
		IssuedAt:             issuedAt,
		ExpiresAt:            expiresAt,
	}

	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return ContactCard{}, fmt.Errorf("marshal contact card payload: %w", err)
	}

	canonicalPayload, err := canonicalizeJCS(payloadRaw)
	if err != nil {
		return ContactCard{}, WrapProtocolError(ErrInvalidContactCard, "canonicalize payload failed: %v", err)
	}

	privKey, err := ParseIdentityPrivateKey(identity.IdentityPrivEd25519)
	if err != nil {
		return ContactCard{}, err
	}

	sigInput := buildContactCardSignInput(canonicalPayload)
	sig, err := privKey.Sign(sigInput)
	if err != nil {
		return ContactCard{}, fmt.Errorf("sign contact card payload: %w", err)
	}

	return ContactCard{
		Payload:   payload,
		SigAlg:    ContactCardSigAlgEd25519,
		SigFormat: ContactCardSigFormatJCS,
		Sig:       encodeBase64URL(sig),
	}, nil
}

func ParseAndVerifyContactCard(raw []byte, now time.Time) (ParsedContactCard, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	if err := mustJSONObject(raw); err != nil {
		return ParsedContactCard{}, err
	}

	var env ContactCardEnvelope
	if err := decodeStrictJSON(raw, &env); err != nil {
		return ParsedContactCard{}, err
	}

	if len(bytes.TrimSpace(env.Payload)) == 0 {
		return ParsedContactCard{}, WrapProtocolError(ErrInvalidContactCard, "payload is required")
	}
	if strings.TrimSpace(env.SigAlg) != ContactCardSigAlgEd25519 {
		return ParsedContactCard{}, WrapProtocolError(ErrInvalidContactCard, "sig_alg must be %q", ContactCardSigAlgEd25519)
	}
	if strings.TrimSpace(env.SigFormat) != ContactCardSigFormatJCS {
		return ParsedContactCard{}, WrapProtocolError(ErrInvalidContactCard, "sig_format must be %q", ContactCardSigFormatJCS)
	}
	if strings.TrimSpace(env.Sig) == "" {
		return ParsedContactCard{}, WrapProtocolError(ErrInvalidContactCard, "sig is required")
	}

	if err := mustJSONObject(env.Payload); err != nil {
		return ParsedContactCard{}, WrapProtocolError(ErrInvalidContactCard, "payload must be JSON object: %v", err)
	}

	var payload ContactCardPayload
	if err := decodeStrictJSON(env.Payload, &payload); err != nil {
		return ParsedContactCard{}, WrapProtocolError(ErrInvalidContactCard, "payload decode failed: %v", err)
	}
	if err := validateContactCardPayload(payload, now); err != nil {
		return ParsedContactCard{}, err
	}

	peerID, err := peer.Decode(payload.PeerID)
	if err != nil {
		return ParsedContactCard{}, WrapProtocolError(ErrInvalidContactCard, "peer_id parse failed: %v", err)
	}

	for _, addr := range payload.Addresses {
		if err := validateAddressMatchesPeerID(addr, peerID); err != nil {
			return ParsedContactCard{}, err
		}
	}

	canonicalPayload, err := canonicalizeJCS(env.Payload)
	if err != nil {
		return ParsedContactCard{}, WrapProtocolError(ErrInvalidContactCard, "canonicalize payload failed: %v", err)
	}

	sigBytes, err := decodeBase64URL(env.Sig)
	if err != nil {
		return ParsedContactCard{}, WrapProtocolError(ErrInvalidContactCard, "sig decode failed: %v", err)
	}

	pubRaw, err := ParseIdentityPublicKey(payload.IdentityPubEd25519)
	if err != nil {
		return ParsedContactCard{}, err
	}
	pub, err := ic.UnmarshalEd25519PublicKey(pubRaw)
	if err != nil {
		return ParsedContactCard{}, WrapProtocolError(ErrInvalidContactCard, "unmarshal identity_pub_ed25519 failed: %v", err)
	}

	verified, err := pub.Verify(buildContactCardSignInput(canonicalPayload), sigBytes)
	if err != nil {
		return ParsedContactCard{}, WrapProtocolError(ErrInvalidContactCard, "signature verification failed: %v", err)
	}
	if !verified {
		return ParsedContactCard{}, WrapProtocolError(ErrInvalidContactCard, "signature mismatch")
	}

	return ParsedContactCard{
		Card: ContactCard{
			Payload:   payload,
			SigAlg:    env.SigAlg,
			SigFormat: env.SigFormat,
			Sig:       env.Sig,
		},
		CanonicalPayload: canonicalPayload,
	}, nil
}

func validateContactCardPayload(payload ContactCardPayload, now time.Time) error {
	if payload.Version != ContactCardVersionV1 {
		return WrapProtocolError(ErrInvalidContactCard, "unsupported payload version: %d", payload.Version)
	}
	if strings.TrimSpace(payload.NodeUUID) == "" {
		return WrapProtocolError(ErrInvalidContactCard, "node_uuid is required")
	}
	if _, err := uuid.Parse(payload.NodeUUID); err != nil {
		return WrapProtocolError(ErrInvalidContactCard, "node_uuid must be a valid UUID: %v", err)
	}
	if strings.TrimSpace(payload.PeerID) == "" {
		return WrapProtocolError(ErrInvalidContactCard, "peer_id is required")
	}
	if strings.TrimSpace(payload.IdentityPubEd25519) == "" {
		return WrapProtocolError(ErrInvalidContactCard, "identity_pub_ed25519 is required")
	}
	if payload.MinSupportedProtocol <= 0 || payload.MaxSupportedProtocol <= 0 {
		return WrapProtocolError(ErrInvalidContactCard, "protocol range must be positive")
	}
	if payload.MinSupportedProtocol > payload.MaxSupportedProtocol {
		return WrapProtocolError(ErrInvalidContactCard, "min_supported_protocol cannot be greater than max_supported_protocol")
	}
	if payload.IssuedAt.IsZero() {
		return WrapProtocolError(ErrInvalidContactCard, "issued_at is required")
	}
	if payload.ExpiresAt != nil && now.After(payload.ExpiresAt.UTC()) {
		return WrapProtocolError(ErrInvalidContactCard, "contact card is expired")
	}

	pubPeerID, err := DerivePeerIDFromIdentityPub(payload.IdentityPubEd25519)
	if err != nil {
		return err
	}
	expectedPeerID, err := peer.Decode(payload.PeerID)
	if err != nil {
		return WrapProtocolError(ErrInvalidContactCard, "peer_id decode failed: %v", err)
	}
	if pubPeerID != expectedPeerID {
		return WrapProtocolError(ErrInvalidContactCard, "peer_id does not match identity_pub_ed25519")
	}

	if payload.NodeID != "" {
		expectedNodeID := NodeIDFromPeerID(expectedPeerID.String())
		if payload.NodeID != expectedNodeID {
			return WrapProtocolError(ErrInvalidContactCard, "node_id mismatch, expected %q", expectedNodeID)
		}
	}

	if len(payload.Addresses) == 0 {
		return WrapProtocolError(ErrInvalidContactCard, "addresses cannot be empty")
	}

	return nil
}

func validateAddressMatchesPeerID(rawAddr string, expectedPeerID peer.ID) error {
	addr := strings.TrimSpace(rawAddr)
	if addr == "" {
		return WrapProtocolError(ErrInvalidContactCard, "address is empty")
	}
	maddr, err := ma.NewMultiaddr(addr)
	if err != nil {
		return WrapProtocolError(ErrInvalidContactCard, "invalid multiaddr %q: %v", addr, err)
	}
	if err := validateDialableAddress(maddr, addr); err != nil {
		return err
	}
	_, last := ma.SplitLast(maddr)
	if last == nil {
		return WrapProtocolError(ErrInvalidContactCard, "multiaddr %q has no terminal component", addr)
	}
	if last.Protocol().Code != ma.P_P2P {
		return WrapProtocolError(ErrInvalidContactCard, "multiaddr %q must end with /p2p/<peer_id>", addr)
	}
	lastPeerID, err := peer.Decode(last.Value())
	if err != nil {
		return WrapProtocolError(ErrInvalidContactCard, "multiaddr %q has invalid /p2p peer id: %v", addr, err)
	}
	if lastPeerID != expectedPeerID {
		return WrapProtocolError(ErrInvalidContactCard, "multiaddr %q terminal peer id mismatch", addr)
	}
	return nil
}

func validateDialableAddress(maddr ma.Multiaddr, rawAddr string) error {
	if value, err := maddr.ValueForProtocol(ma.P_IP4); err == nil {
		if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil && ip.IsUnspecified() {
			return WrapProtocolError(ErrInvalidContactCard, "multiaddr %q is non-dialable: ip4 unspecified", rawAddr)
		}
	}
	if value, err := maddr.ValueForProtocol(ma.P_IP6); err == nil {
		if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil && ip.IsUnspecified() {
			return WrapProtocolError(ErrInvalidContactCard, "multiaddr %q is non-dialable: ip6 unspecified", rawAddr)
		}
	}
	if value, err := maddr.ValueForProtocol(ma.P_TCP); err == nil && strings.TrimSpace(value) == "0" {
		return WrapProtocolError(ErrInvalidContactCard, "multiaddr %q is non-dialable: tcp port 0", rawAddr)
	}
	if value, err := maddr.ValueForProtocol(ma.P_UDP); err == nil && strings.TrimSpace(value) == "0" {
		return WrapProtocolError(ErrInvalidContactCard, "multiaddr %q is non-dialable: udp port 0", rawAddr)
	}
	return nil
}

func normalizeAddresses(addresses []string) []string {
	if len(addresses) == 0 {
		return nil
	}
	out := make([]string, 0, len(addresses))
	seen := map[string]struct{}{}
	for _, raw := range addresses {
		addr := strings.TrimSpace(raw)
		if addr == "" {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	return out
}

func canonicalizeJCS(rawJSON []byte) ([]byte, error) {
	return jsoncanonicalizer.Transform(rawJSON)
}

func buildContactCardSignInput(canonicalPayload []byte) []byte {
	buf := make([]byte, 0, len(ContactCardSignDomainV1)+len(canonicalPayload))
	buf = append(buf, ContactCardSignDomainV1...)
	buf = append(buf, canonicalPayload...)
	return buf
}
