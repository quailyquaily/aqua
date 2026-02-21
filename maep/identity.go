package maep

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	ic "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

func GenerateIdentity(now time.Time) (Identity, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	nodeUUID, err := uuid.NewV7()
	if err != nil {
		return Identity{}, fmt.Errorf("generate uuid v7: %w", err)
	}

	priv, pub, err := ic.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return Identity{}, fmt.Errorf("generate ed25519 keypair: %w", err)
	}

	peerID, err := peer.IDFromPublicKey(pub)
	if err != nil {
		return Identity{}, fmt.Errorf("derive peer id from public key: %w", err)
	}

	pubRaw, err := pub.Raw()
	if err != nil {
		return Identity{}, fmt.Errorf("export public key bytes: %w", err)
	}
	if len(pubRaw) != ed25519PublicKeyBytes {
		return Identity{}, fmt.Errorf("invalid ed25519 public key bytes length: %d", len(pubRaw))
	}

	privRaw, err := priv.Raw()
	if err != nil {
		return Identity{}, fmt.Errorf("export private key bytes: %w", err)
	}

	return Identity{
		NodeUUID:            nodeUUID.String(),
		PeerID:              peerID.String(),
		NodeID:              NodeIDFromPeerID(peerID.String()),
		IdentityPubEd25519:  encodeBase64URL(pubRaw),
		IdentityPrivEd25519: encodeBase64URL(privRaw),
		CreatedAt:           now,
		UpdatedAt:           now,
	}, nil
}

func ParseIdentityPublicKey(identityPubEd25519 string) ([]byte, error) {
	pubRaw, err := decodeBase64URL(identityPubEd25519)
	if err != nil {
		return nil, WrapProtocolError(ErrInvalidContactCard, "identity_pub_ed25519 decode failed: %v", err)
	}
	if len(pubRaw) != ed25519PublicKeyBytes {
		return nil, WrapProtocolError(ErrInvalidContactCard, "identity_pub_ed25519 must decode to %d bytes, got %d", ed25519PublicKeyBytes, len(pubRaw))
	}
	return pubRaw, nil
}

func ParseIdentityPrivateKey(identityPrivEd25519 string) (ic.PrivKey, error) {
	privRaw, err := decodeBase64URL(identityPrivEd25519)
	if err != nil {
		return nil, fmt.Errorf("identity_priv_ed25519 decode failed: %w", err)
	}
	priv, err := ic.UnmarshalEd25519PrivateKey(privRaw)
	if err != nil {
		return nil, fmt.Errorf("unmarshal ed25519 private key: %w", err)
	}
	return priv, nil
}

func DerivePeerIDFromIdentityPub(identityPubEd25519 string) (peer.ID, error) {
	pubRaw, err := ParseIdentityPublicKey(identityPubEd25519)
	if err != nil {
		return "", err
	}
	pub, err := ic.UnmarshalEd25519PublicKey(pubRaw)
	if err != nil {
		return "", WrapProtocolError(ErrInvalidContactCard, "unmarshal ed25519 public key failed: %v", err)
	}
	derivedPeerID, err := peer.IDFromPublicKey(pub)
	if err != nil {
		return "", WrapProtocolError(ErrInvalidContactCard, "derive peer id failed: %v", err)
	}
	return derivedPeerID, nil
}

func FingerprintHex(identityPubEd25519 string) (string, error) {
	pubRaw, err := ParseIdentityPublicKey(identityPubEd25519)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(pubRaw)
	return hex.EncodeToString(sum[:]), nil
}

func FingerprintGrouped(identityPubEd25519 string) (string, error) {
	hexFingerprint, err := FingerprintHex(identityPubEd25519)
	if err != nil {
		return "", err
	}
	if len(hexFingerprint) != 64 {
		return hexFingerprint, nil
	}
	parts := make([]string, 0, 8)
	for i := 0; i < len(hexFingerprint); i += 8 {
		parts = append(parts, hexFingerprint[i:i+8])
	}
	return strings.Join(parts, "-"), nil
}

func NodeIDFromPeerID(peerID string) string {
	return NodeIDPrefix + strings.TrimSpace(peerID)
}

func encodeBase64URL(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeBase64URL(encoded string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
}
