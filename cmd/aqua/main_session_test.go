package main

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestResolveOrGenerateSessionID(t *testing.T) {
	t.Parallel()

	t.Run("generates uuid_v7 when empty", func(t *testing.T) {
		t.Parallel()

		sessionID, err := resolveOrGenerateSessionID("")
		if err != nil {
			t.Fatalf("resolveOrGenerateSessionID returned error: %v", err)
		}
		parsed, err := uuid.Parse(sessionID)
		if err != nil {
			t.Fatalf("generated session_id parse failed: %v", err)
		}
		if parsed.Version() != uuid.Version(7) {
			t.Fatalf("expected generated uuid_v7, got version=%d (%s)", parsed.Version(), sessionID)
		}
	})

	t.Run("keeps provided uuid_v7", func(t *testing.T) {
		t.Parallel()

		id, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("uuid.NewV7 failed: %v", err)
		}
		got, err := resolveOrGenerateSessionID(id.String())
		if err != nil {
			t.Fatalf("resolveOrGenerateSessionID returned error: %v", err)
		}
		if got != id.String() {
			t.Fatalf("session_id changed: got %s want %s", got, id.String())
		}
	})

	t.Run("rejects non uuid_v7", func(t *testing.T) {
		t.Parallel()

		_, err := resolveOrGenerateSessionID(uuid.NewString())
		if err == nil {
			t.Fatalf("expected error for non-uuid_v7 session_id")
		}
		if !strings.Contains(err.Error(), "uuid_v7") {
			t.Fatalf("expected uuid_v7 error, got %v", err)
		}
	})

	t.Run("rejects invalid uuid", func(t *testing.T) {
		t.Parallel()

		_, err := resolveOrGenerateSessionID("not-a-uuid")
		if err == nil {
			t.Fatalf("expected error for invalid uuid")
		}
		if !strings.Contains(err.Error(), "uuid_v7") {
			t.Fatalf("expected uuid_v7 error, got %v", err)
		}
	})
}
