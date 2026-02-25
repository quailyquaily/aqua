package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quailyquaily/aqua/aqua"
)

func executeCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := newRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestIDAutoInit_NoNickname(t *testing.T) {
	dir := t.TempDir()

	stdout, stderr, err := executeCLI(t, "--dir", dir, "id", "--json")
	if err != nil {
		t.Fatalf("id --json error = %v, stderr=%s", err, stderr)
	}

	var view map[string]any
	if err := json.Unmarshal([]byte(stdout), &view); err != nil {
		t.Fatalf("decode id output json error = %v, stdout=%s", err, stdout)
	}
	peerID, _ := view["peer_id"].(string)
	if strings.TrimSpace(peerID) == "" {
		t.Fatalf("peer_id should not be empty in id output")
	}

	svc := aqua.NewService(aqua.NewFileStore(dir))
	identity, ok, err := svc.GetIdentity(context.Background())
	if err != nil {
		t.Fatalf("GetIdentity() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected identity to be auto-created by `aqua id`")
	}
	if strings.TrimSpace(identity.PeerID) == "" {
		t.Fatalf("stored identity peer_id should not be empty")
	}
}

func TestIDAutoInit_WithNickname(t *testing.T) {
	dir := t.TempDir()

	stdout, stderr, err := executeCLI(t, "--dir", dir, "id", "alice", "--json")
	if err != nil {
		t.Fatalf("id alice --json error = %v, stderr=%s", err, stderr)
	}

	var view map[string]any
	if err := json.Unmarshal([]byte(stdout), &view); err != nil {
		t.Fatalf("decode id output json error = %v, stdout=%s", err, stdout)
	}
	nickname, _ := view["nickname"].(string)
	if nickname != "alice" {
		t.Fatalf("nickname mismatch: got %q want %q", nickname, "alice")
	}

	svc := aqua.NewService(aqua.NewFileStore(dir))
	identity, ok, err := svc.GetIdentity(context.Background())
	if err != nil {
		t.Fatalf("GetIdentity() error = %v", err)
	}
	if !ok {
		t.Fatalf("expected identity to be auto-created by `aqua id alice`")
	}
	if identity.Nickname != "alice" {
		t.Fatalf("stored nickname mismatch: got %q want %q", identity.Nickname, "alice")
	}
}

func TestInboxListUnreadAutoMarksRead(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	store := aqua.NewFileStore(dir)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	receivedAt := time.Date(2026, 2, 22, 14, 0, 0, 0, time.UTC)
	if err := store.AppendInboxMessage(ctx, aqua.InboxMessage{
		MessageID:      "msg-unread-1",
		FromPeerID:     "12D3KooWpeerA",
		Topic:          "chat.message",
		ContentType:    "text/plain",
		PayloadBase64:  "aGVsbG8",
		IdempotencyKey: "k-1",
		SessionID:      "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456",
		ReceivedAt:     receivedAt,
	}); err != nil {
		t.Fatalf("AppendInboxMessage() error = %v", err)
	}

	stdout, stderr, err := executeCLI(t, "--dir", dir, "inbox", "list", "--unread", "--json")
	if err != nil {
		t.Fatalf("inbox list --unread --json error = %v, stderr=%s", err, stderr)
	}

	var records []aqua.InboxMessage
	if err := json.Unmarshal([]byte(stdout), &records); err != nil {
		t.Fatalf("decode inbox output json error = %v, stdout=%s", err, stdout)
	}
	if len(records) != 1 {
		t.Fatalf("unread list length mismatch: got %d want 1", len(records))
	}
	if records[0].MessageID != "msg-unread-1" {
		t.Fatalf("unexpected message_id: got %q", records[0].MessageID)
	}
	if records[0].Read {
		t.Fatalf("listed unread message should be shown as unread in current output")
	}

	svc := aqua.NewService(aqua.NewFileStore(dir))
	afterFirstRead, err := svc.ListInboxMessages(ctx, "", "", 10)
	if err != nil {
		t.Fatalf("ListInboxMessages() error = %v", err)
	}
	if len(afterFirstRead) != 1 {
		t.Fatalf("stored inbox length mismatch: got %d want 1", len(afterFirstRead))
	}
	if !afterFirstRead[0].Read {
		t.Fatalf("message should be auto-marked read after unread listing")
	}
	if afterFirstRead[0].ReadAt == nil {
		t.Fatalf("message read_at should be set after unread listing")
	}

	stdout2, stderr2, err := executeCLI(t, "--dir", dir, "inbox", "list", "--unread", "--json")
	if err != nil {
		t.Fatalf("second inbox list --unread --json error = %v, stderr=%s", err, stderr2)
	}
	var records2 []aqua.InboxMessage
	if err := json.Unmarshal([]byte(stdout2), &records2); err != nil {
		t.Fatalf("decode second inbox output json error = %v, stdout=%s", err, stdout2)
	}
	if len(records2) != 0 {
		t.Fatalf("second unread list should be empty, got %d", len(records2))
	}
}

func TestInboxListJSONIncludesPayloadTextForTextContent(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	store := aqua.NewFileStore(dir)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	payloadText := "hello inbox"
	if err := store.AppendInboxMessage(ctx, aqua.InboxMessage{
		MessageID:      "msg-text-1",
		FromPeerID:     "12D3KooWpeerText",
		Topic:          "chat.message",
		ContentType:    "text/plain; charset=utf-8",
		PayloadBase64:  base64.RawURLEncoding.EncodeToString([]byte(payloadText)),
		IdempotencyKey: "idem-text-1",
		SessionID:      "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456",
		ReceivedAt:     time.Date(2026, 2, 22, 15, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AppendInboxMessage(text) error = %v", err)
	}

	stdout, stderr, err := executeCLI(t, "--dir", dir, "inbox", "list", "--json")
	if err != nil {
		t.Fatalf("inbox list --json error = %v, stderr=%s", err, stderr)
	}

	var records []map[string]any
	if err := json.Unmarshal([]byte(stdout), &records); err != nil {
		t.Fatalf("decode inbox list json error = %v, stdout=%s", err, stdout)
	}
	if len(records) != 1 {
		t.Fatalf("inbox list length mismatch: got %d want 1", len(records))
	}
	if got, _ := records[0]["payload_text"].(string); got != payloadText {
		t.Fatalf("payload_text mismatch: got %q want %q", got, payloadText)
	}
	if got, _ := records[0]["payload_base64"].(string); got == "" {
		t.Fatalf("payload_base64 should remain present")
	}
}

func TestOutboxListJSONIncludesPayloadTextForJSONContent(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	store := aqua.NewFileStore(dir)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	payloadText := `{"kind":"note","body":"hello"}`
	if err := store.AppendOutboxMessage(ctx, aqua.OutboxMessage{
		MessageID:      "msg-out-text-1",
		ToPeerID:       "12D3KooWpeerOutbox",
		Topic:          "chat.message",
		ContentType:    "application/ld+json; profile=test",
		PayloadBase64:  base64.RawURLEncoding.EncodeToString([]byte(payloadText)),
		IdempotencyKey: "idem-out-text-1",
		SessionID:      "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456",
		SentAt:         time.Date(2026, 2, 22, 16, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AppendOutboxMessage(textual json) error = %v", err)
	}

	stdout, stderr, err := executeCLI(t, "--dir", dir, "outbox", "list", "--json")
	if err != nil {
		t.Fatalf("outbox list --json error = %v, stderr=%s", err, stderr)
	}

	var records []map[string]any
	if err := json.Unmarshal([]byte(stdout), &records); err != nil {
		t.Fatalf("decode outbox list json error = %v, stdout=%s", err, stdout)
	}
	if len(records) != 1 {
		t.Fatalf("outbox list length mismatch: got %d want 1", len(records))
	}
	if got, _ := records[0]["payload_text"].(string); got != payloadText {
		t.Fatalf("payload_text mismatch: got %q want %q", got, payloadText)
	}
	if got, _ := records[0]["payload_base64"].(string); got == "" {
		t.Fatalf("payload_base64 should remain present")
	}
}

func TestServeDryRunJSON(t *testing.T) {
	dir := t.TempDir()

	stdout, stderr, err := executeCLI(t, "--dir", dir, "serve", "--dryrun", "--json")
	if err != nil {
		t.Fatalf("serve --dryrun --json error = %v, stderr=%s", err, stderr)
	}

	var view map[string]any
	if err := json.Unmarshal([]byte(stdout), &view); err != nil {
		t.Fatalf("decode serve dryrun output json error = %v, stdout=%s", err, stdout)
	}
	if got, _ := view["status"].(string); got != "dryrun" {
		t.Fatalf("status mismatch: got %q want %q", got, "dryrun")
	}
	if dryrun, ok := view["dryrun"].(bool); !ok || !dryrun {
		t.Fatalf("dryrun flag should be true in output, got %v", view["dryrun"])
	}
	peerID, _ := view["peer_id"].(string)
	if strings.TrimSpace(peerID) == "" {
		t.Fatalf("peer_id should not be empty in dryrun output")
	}
	addresses, ok := view["addresses"].([]any)
	if !ok || len(addresses) == 0 {
		t.Fatalf("addresses should be non-empty in dryrun output: %#v", view["addresses"])
	}
}

func TestServeDryRunJSON_WithRelayBuildsCircuitAddress(t *testing.T) {
	dir := t.TempDir()
	relayIdentity, err := aqua.GenerateIdentity(time.Date(2026, 2, 22, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateIdentity(relay) error = %v", err)
	}
	relayEndpoint := fmt.Sprintf("/dns4/relay.example.com/tcp/6372/p2p/%s", relayIdentity.PeerID)

	stdout, stderr, err := executeCLI(
		t,
		"--dir", dir,
		"serve",
		"--dryrun",
		"--json",
		"--listen", "/ip4/127.0.0.1/tcp/6372",
		"--relay", relayEndpoint,
		"--relay-mode", "required",
	)
	if err != nil {
		t.Fatalf("serve --dryrun relay --json error = %v, stderr=%s", err, stderr)
	}

	var view map[string]any
	if err := json.Unmarshal([]byte(stdout), &view); err != nil {
		t.Fatalf("decode serve dryrun relay output json error = %v, stdout=%s", err, stdout)
	}
	peerID, _ := view["peer_id"].(string)
	if strings.TrimSpace(peerID) == "" {
		t.Fatalf("peer_id should not be empty in dryrun output")
	}

	wantDirect := fmt.Sprintf("/ip4/127.0.0.1/tcp/6372/p2p/%s", peerID)
	wantRelay := fmt.Sprintf("/dns4/relay.example.com/tcp/6372/p2p/%s/p2p-circuit/p2p/%s", relayIdentity.PeerID, peerID)

	addresses, ok := view["addresses"].([]any)
	if !ok || len(addresses) == 0 {
		t.Fatalf("addresses should be non-empty in dryrun output: %#v", view["addresses"])
	}
	gotSet := map[string]bool{}
	for _, item := range addresses {
		value, _ := item.(string)
		if value != "" {
			gotSet[value] = true
		}
	}
	if !gotSet[wantDirect] {
		t.Fatalf("missing direct address %q in %v", wantDirect, gotSet)
	}
	if !gotSet[wantRelay] {
		t.Fatalf("missing relay circuit address %q in %v", wantRelay, gotSet)
	}
}

func TestContactsImportJSON(t *testing.T) {
	dir := t.TempDir()
	cardPath, remotePeerID := writeContactCardFixture(t)

	stdout, stderr, err := executeCLI(t, "--dir", dir, "contacts", "import", cardPath, "--json")
	if err != nil {
		t.Fatalf("contacts import --json error = %v, stderr=%s", err, stderr)
	}

	var view map[string]any
	if err := json.Unmarshal([]byte(stdout), &view); err != nil {
		t.Fatalf("decode contacts import json error = %v, stdout=%s", err, stdout)
	}
	if got, _ := view["status"].(string); got != "created" {
		t.Fatalf("status mismatch: got %q want %q", got, "created")
	}
	if got, _ := view["peer_id"].(string); got != remotePeerID {
		t.Fatalf("peer_id mismatch: got %q want %q", got, remotePeerID)
	}
	if got, _ := view["trust_state"].(string); got != string(aqua.TrustStateTOFU) {
		t.Fatalf("trust_state mismatch: got %q want %q", got, aqua.TrustStateTOFU)
	}
}

func TestContactsVerifyJSON(t *testing.T) {
	dir := t.TempDir()
	contact := seedImportedContact(t, dir)

	stdout, stderr, err := executeCLI(t, "--dir", dir, "contacts", "verify", contact.PeerID, "--json")
	if err != nil {
		t.Fatalf("contacts verify --json error = %v, stderr=%s", err, stderr)
	}

	var view map[string]any
	if err := json.Unmarshal([]byte(stdout), &view); err != nil {
		t.Fatalf("decode contacts verify json error = %v, stdout=%s", err, stdout)
	}
	if got, _ := view["peer_id"].(string); got != contact.PeerID {
		t.Fatalf("peer_id mismatch: got %q want %q", got, contact.PeerID)
	}
	if got, _ := view["trust_state"].(string); got != string(aqua.TrustStateVerified) {
		t.Fatalf("trust_state mismatch: got %q want %q", got, aqua.TrustStateVerified)
	}
}

func TestContactsDelJSON(t *testing.T) {
	dir := t.TempDir()
	contact := seedImportedContact(t, dir)

	stdout, stderr, err := executeCLI(t, "--dir", dir, "contacts", "del", contact.PeerID, "--json")
	if err != nil {
		t.Fatalf("contacts del --json error = %v, stderr=%s", err, stderr)
	}

	var view map[string]any
	if err := json.Unmarshal([]byte(stdout), &view); err != nil {
		t.Fatalf("decode contacts del json error = %v, stdout=%s", err, stdout)
	}
	if deleted, ok := view["deleted"].(bool); !ok || !deleted {
		t.Fatalf("deleted flag mismatch: got %#v", view["deleted"])
	}
	if got, _ := view["peer_id"].(string); got != contact.PeerID {
		t.Fatalf("peer_id mismatch: got %q want %q", got, contact.PeerID)
	}

	svc := aqua.NewService(aqua.NewFileStore(dir))
	_, ok, err := svc.GetContactByPeerID(context.Background(), contact.PeerID)
	if err != nil {
		t.Fatalf("GetContactByPeerID() error = %v", err)
	}
	if ok {
		t.Fatalf("expected contact to be deleted")
	}
}

func writeContactCardFixture(t *testing.T) (string, string) {
	t.Helper()

	now := time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC)
	remoteSvc := aqua.NewService(aqua.NewFileStore(filepath.Join(t.TempDir(), "remote")))
	identity, _, err := remoteSvc.EnsureIdentity(context.Background(), now)
	if err != nil {
		t.Fatalf("EnsureIdentity(remote) error = %v", err)
	}
	addresses := []string{fmt.Sprintf("/ip4/127.0.0.1/tcp/6372/p2p/%s", identity.PeerID)}
	_, rawCard, err := remoteSvc.ExportContactCard(
		context.Background(),
		addresses,
		aqua.ProtocolVersionV1,
		aqua.ProtocolVersionV1,
		now,
		nil,
	)
	if err != nil {
		t.Fatalf("ExportContactCard(remote) error = %v", err)
	}
	cardPath := filepath.Join(t.TempDir(), "contact_card.json")
	if err := os.WriteFile(cardPath, rawCard, 0o600); err != nil {
		t.Fatalf("WriteFile(contact_card) error = %v", err)
	}
	return cardPath, identity.PeerID
}

func seedImportedContact(t *testing.T, dir string) aqua.Contact {
	t.Helper()

	now := time.Date(2026, 2, 23, 12, 5, 0, 0, time.UTC)
	remoteSvc := aqua.NewService(aqua.NewFileStore(filepath.Join(t.TempDir(), "remote-seed")))
	remoteIdentity, _, err := remoteSvc.EnsureIdentity(context.Background(), now)
	if err != nil {
		t.Fatalf("EnsureIdentity(remote seed) error = %v", err)
	}
	addresses := []string{fmt.Sprintf("/ip4/127.0.0.1/tcp/6372/p2p/%s", remoteIdentity.PeerID)}
	_, rawCard, err := remoteSvc.ExportContactCard(
		context.Background(),
		addresses,
		aqua.ProtocolVersionV1,
		aqua.ProtocolVersionV1,
		now,
		nil,
	)
	if err != nil {
		t.Fatalf("ExportContactCard(remote seed) error = %v", err)
	}

	localSvc := aqua.NewService(aqua.NewFileStore(dir))
	if _, _, err := localSvc.EnsureIdentity(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatalf("EnsureIdentity(local seed) error = %v", err)
	}
	result, err := localSvc.ImportContactCard(context.Background(), rawCard, "", now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("ImportContactCard(local seed) error = %v", err)
	}
	return result.Contact
}
