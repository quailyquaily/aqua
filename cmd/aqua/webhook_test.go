package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quailyquaily/aqua/aqua"
)

func TestResolveServeWebhookURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "empty", value: "", want: ""},
		{name: "https", value: "https://example.com/hooks/aqua", want: "https://example.com/hooks/aqua"},
		{name: "http", value: "http://127.0.0.1:8080/inbox", want: "http://127.0.0.1:8080/inbox"},
		{name: "missing scheme", value: "example.com/hooks", wantErr: true},
		{name: "unsupported scheme", value: "ftp://example.com/hooks", wantErr: true},
		{name: "missing host", value: "https:///hooks", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveServeWebhookURL(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveServeWebhookURL() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveServeWebhookURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveServeWebhookURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDataPushWebhookDispatcherRetriesUntilSuccess(t *testing.T) {
	var attempts atomic.Int32
	delivered := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		attempt := attempts.Add(1)
		if attempt < 3 {
			http.Error(w, "try later", http.StatusBadGateway)
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		select {
		case delivered <- payload:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dispatcher, err := newDataPushWebhookDispatcher(context.Background(), server.URL, logger)
	if err != nil {
		t.Fatalf("newDataPushWebhookDispatcher() error = %v", err)
	}
	dispatcher.retryInitialBackoff = 5 * time.Millisecond
	dispatcher.retryMaximumBackoff = 10 * time.Millisecond
	defer dispatcher.close()

	payload := []byte("hello webhook")
	event := aqua.DataPushEvent{
		FromPeerID:     "peer-1",
		Topic:          "chat.message",
		ContentType:    "text/plain",
		PayloadBase64:  base64.RawURLEncoding.EncodeToString(payload),
		PayloadBytes:   payload,
		IdempotencyKey: "idem-1",
		SessionID:      "session-1",
		ReplyTo:        "reply-1",
		ReceivedAt:     time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC),
		Deduped:        false,
	}
	dispatcher.enqueueDataPush(event)

	select {
	case got := <-delivered:
		if attempts.Load() != 3 {
			t.Fatalf("attempt count = %d, want 3", attempts.Load())
		}
		if got["event"] != "agent.data.push" {
			t.Fatalf("event = %v, want agent.data.push", got["event"])
		}
		if got["from_peer_id"] != event.FromPeerID {
			t.Fatalf("from_peer_id = %v, want %q", got["from_peer_id"], event.FromPeerID)
		}
		if got["topic"] != event.Topic {
			t.Fatalf("topic = %v, want %q", got["topic"], event.Topic)
		}
		if got["payload_text"] != string(payload) {
			t.Fatalf("payload_text = %v, want %q", got["payload_text"], string(payload))
		}
		if got["payload_base64"] != event.PayloadBase64 {
			t.Fatalf("payload_base64 = %v, want %q", got["payload_base64"], event.PayloadBase64)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for webhook delivery")
	}
}

func TestDataPushWebhookDispatcherDeliversContactControlEvent(t *testing.T) {
	delivered := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		delivered <- payload
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dispatcher, err := newDataPushWebhookDispatcher(context.Background(), server.URL, logger)
	if err != nil {
		t.Fatalf("newDataPushWebhookDispatcher() error = %v", err)
	}
	defer dispatcher.close()

	receivedAt := time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 3, 12, 11, 59, 0, 0, time.UTC)
	dispatcher.enqueueContactControl(aqua.ContactControlEvent{
		FromPeerID:     "peer-contact-1",
		ToPeerID:       "peer-contact-2",
		Action:         aqua.ContactControlActionRequest,
		RequestID:      "creq_123",
		VerifyOnAccept: true,
		CreatedAt:      &createdAt,
		ReceivedAt:     receivedAt,
	})

	select {
	case got := <-delivered:
		if got["event"] != "agent.contact.push" {
			t.Fatalf("event = %v, want agent.contact.push", got["event"])
		}
		if got["action"] != string(aqua.ContactControlActionRequest) {
			t.Fatalf("action = %v, want %q", got["action"], aqua.ContactControlActionRequest)
		}
		if got["request_id"] != "creq_123" {
			t.Fatalf("request_id = %v, want creq_123", got["request_id"])
		}
		if got["from_peer_id"] != "peer-contact-1" {
			t.Fatalf("from_peer_id = %v, want peer-contact-1", got["from_peer_id"])
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for contact control webhook delivery")
	}
}
