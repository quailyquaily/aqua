package maep

import (
	"testing"
)

func TestValidateSessionForTopic_DialogueRequiresSessionID(t *testing.T) {
	_, _, err := validateSessionForTopic("dm.reply.v1", "", "")
	if err == nil {
		t.Fatalf("expected error when dialogue topic is missing session_id")
	}
}

func TestValidateSessionForTopic_DialogueAcceptsSessionID(t *testing.T) {
	sessionID, replyTo, err := validateSessionForTopic("dm.reply.v1", "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456", "msg_prev")
	if err != nil {
		t.Fatalf("validateSessionForTopic() error = %v", err)
	}
	if sessionID != "0194f5c0-8f6e-7d9d-a4d7-6d8d4f35f456" {
		t.Fatalf("session_id mismatch: got %q", sessionID)
	}
	if replyTo != "msg_prev" {
		t.Fatalf("reply_to mismatch: got %q", replyTo)
	}
}

func TestValidateSessionForTopic_NonDialogueAllowsMissingSessionID(t *testing.T) {
	sessionID, replyTo, err := validateSessionForTopic("agent.status.v1", "", "")
	if err != nil {
		t.Fatalf("validateSessionForTopic() error = %v", err)
	}
	if sessionID != "" {
		t.Fatalf("expected empty session_id, got %q", sessionID)
	}
	if replyTo != "" {
		t.Fatalf("expected empty reply_to, got %q", replyTo)
	}
}

func TestValidateSessionForTopic_RejectsNonUUIDv7SessionID(t *testing.T) {
	_, _, err := validateSessionForTopic("dm.reply.v1", "peerA::dialogue.v1", "")
	if err == nil {
		t.Fatalf("expected error for non-uuid_v7 session_id")
	}
}

func TestValidateSessionForTopic_TrimReplyTo(t *testing.T) {
	_, replyTo, err := validateSessionForTopic("agent.status.v1", "", "  msg_prev  ")
	if err != nil {
		t.Fatalf("validateSessionForTopic() error = %v", err)
	}
	if replyTo != "msg_prev" {
		t.Fatalf("reply_to mismatch: got %q", replyTo)
	}
}
