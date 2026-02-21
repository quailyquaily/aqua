package maep

import (
	"encoding/json"
	"testing"
)

func TestExtractAndValidateSessionForTopic_DialogueRequiresSessionID(t *testing.T) {
	payload := map[string]any{
		"message_id": "msg_1",
		"text":       "hello",
		"sent_at":    "2026-02-08T01:02:03Z",
	}
	raw, _ := json.Marshal(payload)

	_, _, err := extractAndValidateSessionForTopic("dm.reply.v1", "application/json", raw)
	if err == nil {
		t.Fatalf("expected error when dialogue topic is missing session_id")
	}
}

func TestExtractAndValidateSessionForTopic_DialogueAcceptsSessionID(t *testing.T) {
	payload := map[string]any{
		"message_id": "msg_2",
		"text":       "hello",
		"sent_at":    "2026-02-08T01:02:03Z",
		"session_id": "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456",
		"reply_to":   "msg_prev",
	}
	raw, _ := json.Marshal(payload)

	sessionID, replyTo, err := extractAndValidateSessionForTopic("dm.reply.v1", "application/json", raw)
	if err != nil {
		t.Fatalf("extractAndValidateSessionForTopic() error = %v", err)
	}
	if sessionID != "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456" {
		t.Fatalf("session_id mismatch: got %q", sessionID)
	}
	if replyTo != "msg_prev" {
		t.Fatalf("reply_to mismatch: got %q", replyTo)
	}
}

func TestExtractAndValidateSessionForTopic_NonDialogueAllowsMissingSessionID(t *testing.T) {
	payload := map[string]any{
		"message_id": "msg_3",
		"text":       "hello",
		"sent_at":    "2026-02-08T01:02:03Z",
	}
	raw, _ := json.Marshal(payload)

	sessionID, replyTo, err := extractAndValidateSessionForTopic("agent.status.v1", "application/json", raw)
	if err != nil {
		t.Fatalf("extractAndValidateSessionForTopic() error = %v", err)
	}
	if sessionID != "" {
		t.Fatalf("expected empty session_id, got %q", sessionID)
	}
	if replyTo != "" {
		t.Fatalf("expected empty reply_to, got %q", replyTo)
	}
}

func TestExtractAndValidateSessionForTopic_RejectsNonUUIDv7SessionID(t *testing.T) {
	payload := map[string]any{
		"message_id": "msg_4",
		"text":       "hello",
		"sent_at":    "2026-02-08T01:02:03Z",
		"session_id": "peerA::dialogue.v1",
	}
	raw, _ := json.Marshal(payload)

	_, _, err := extractAndValidateSessionForTopic("dm.reply.v1", "application/json", raw)
	if err == nil {
		t.Fatalf("expected error for non-uuid_v7 session_id")
	}
}

func TestExtractAndValidateSessionForTopic_RejectsPlainTextContentType(t *testing.T) {
	_, _, err := extractAndValidateSessionForTopic("dm.reply.v1", "text/plain", []byte("hello"))
	if err == nil {
		t.Fatalf("expected error for plain-text content type")
	}
}

func TestExtractAndValidateSessionForTopic_RequiresEnvelopeFields(t *testing.T) {
	payload := map[string]any{
		"text":    "hello",
		"sent_at": "2026-02-08T01:02:03Z",
	}
	raw, _ := json.Marshal(payload)
	_, _, err := extractAndValidateSessionForTopic("agent.status.v1", "application/json", raw)
	if err == nil {
		t.Fatalf("expected error for missing message_id")
	}
}
