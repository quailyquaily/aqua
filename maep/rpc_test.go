package maep

import (
	"encoding/json"
	"testing"
)

func TestExtractRPCIDForError_StringID(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":"req-1","method":"agent.ping","params":{}}`)
	id, ok := extractRPCIDForError(raw)
	if !ok {
		t.Fatalf("expected id to be extracted")
	}
	got, typeOK := id.(string)
	if !typeOK {
		t.Fatalf("expected string id, got %T", id)
	}
	if got != "req-1" {
		t.Fatalf("id mismatch: got %q want %q", got, "req-1")
	}
}

func TestExtractRPCIDForError_IntegerID(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":7,"method":"agent.ping","params":{}}`)
	id, ok := extractRPCIDForError(raw)
	if !ok {
		t.Fatalf("expected id to be extracted")
	}
	got, typeOK := id.(json.Number)
	if !typeOK {
		t.Fatalf("expected json.Number id, got %T", id)
	}
	if got.String() != "7" {
		t.Fatalf("id mismatch: got %q want %q", got.String(), "7")
	}
}

func TestExtractRPCIDForError_InvalidID(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":1.5,"method":"agent.ping","params":{}}`)
	if _, ok := extractRPCIDForError(raw); ok {
		t.Fatalf("expected invalid id to be rejected")
	}
}

func TestExtractRPCIDForError_BestEffortForSemanticFailure(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":"req-2","params":null}`)
	id, ok := extractRPCIDForError(raw)
	if !ok {
		t.Fatalf("expected id to be extracted")
	}
	got, typeOK := id.(string)
	if !typeOK {
		t.Fatalf("expected string id, got %T", id)
	}
	if got != "req-2" {
		t.Fatalf("id mismatch: got %q want %q", got, "req-2")
	}
}

func TestExtractRPCIDForError_InvalidJSON(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":"req-3",`)
	if _, ok := extractRPCIDForError(raw); ok {
		t.Fatalf("expected invalid json to return no id")
	}
}
