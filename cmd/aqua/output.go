package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/quailyquaily/aqua/aqua"
	"github.com/spf13/cobra"
)

func printDataPushEvent(cmd *cobra.Command, event aqua.DataPushEvent, outputJSON bool) {
	if outputJSON {
		payloadText := ""
		if strings.HasPrefix(strings.ToLower(event.ContentType), "text/") || strings.EqualFold(event.ContentType, "application/json") {
			payloadText = string(event.PayloadBytes)
		}
		_ = writeJSON(cmd.OutOrStdout(), map[string]any{
			"event":           "agent.data.push",
			"from_peer_id":    event.FromPeerID,
			"topic":           event.Topic,
			"content_type":    event.ContentType,
			"idempotency_key": event.IdempotencyKey,
			"session_id":      event.SessionID,
			"reply_to":        event.ReplyTo,
			"deduped":         event.Deduped,
			"received_at":     event.ReceivedAt,
			"payload_text":    payloadText,
			"payload_base64":  event.PayloadBase64,
		})
		return
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "incoming: topic=%s from=%s session_id=%s deduped=%v idempotency_key=%s\n", event.Topic, event.FromPeerID, event.SessionID, event.Deduped, event.IdempotencyKey)
	if strings.HasPrefix(strings.ToLower(event.ContentType), "application/json") {
		var obj any
		if err := json.Unmarshal(event.PayloadBytes, &obj); err == nil {
			pretty, _ := json.MarshalIndent(obj, "", "  ")
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "payload(json): %s\n", string(pretty))
			return
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "payload(text): %s\n", string(event.PayloadBytes))
		return
	}
	if strings.HasPrefix(strings.ToLower(event.ContentType), "text/") {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "payload(text): %s\n", string(event.PayloadBytes))
		return
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "payload(bytes): %d\n", len(event.PayloadBytes))
}

func summarizePayload(contentType string, payloadBase64 string) string {
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(payloadBase64))
	if err != nil {
		return "<invalid-base64>"
	}
	lowerType := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(lowerType, "application/json") {
		var obj any
		if err := json.Unmarshal(data, &obj); err != nil {
			return string(data)
		}
		text, err := json.Marshal(obj)
		if err != nil {
			return "<json-encode-error>"
		}
		return string(text)
	}
	if strings.HasPrefix(lowerType, "text/") {
		return string(data)
	}
	return fmt.Sprintf("<%d bytes>", len(data))
}

func writeInboxRecords(w io.Writer, records []aqua.InboxMessage) {
	for i, record := range records {
		payloadSummary := summarizePayload(record.ContentType, record.PayloadBase64)
		_, _ = fmt.Fprintf(w, "[%d]\n", i+1)
		_, _ = fmt.Fprintf(w, "received_at: %s\n", record.ReceivedAt.UTC().Format(time.RFC3339))
		status := "unread"
		if record.Read {
			status = "read"
		}
		_, _ = fmt.Fprintf(w, "status: %s\n", status)
		if record.ReadAt != nil {
			_, _ = fmt.Fprintf(w, "read_at: %s\n", record.ReadAt.UTC().Format(time.RFC3339))
		}
		_, _ = fmt.Fprintf(w, "from_peer_id: %s\n", record.FromPeerID)
		_, _ = fmt.Fprintf(w, "topic: %s\n", record.Topic)
		_, _ = fmt.Fprintf(w, "session_id: %s\n", record.SessionID)
		if strings.TrimSpace(record.ReplyTo) != "" {
			_, _ = fmt.Fprintf(w, "reply_to: %s\n", record.ReplyTo)
		}
		_, _ = fmt.Fprintf(w, "idempotency_key: %s\n", record.IdempotencyKey)
		_, _ = fmt.Fprintf(w, "content_type: %s\n", record.ContentType)
		_, _ = fmt.Fprintln(w, "payload:")
		_, _ = fmt.Fprintln(w, indentBlock(payloadSummary, "  "))
		if i < len(records)-1 {
			_, _ = fmt.Fprintln(w, "")
		}
	}
}

func writeOutboxRecords(w io.Writer, records []aqua.OutboxMessage) {
	for i, record := range records {
		payloadSummary := summarizePayload(record.ContentType, record.PayloadBase64)
		_, _ = fmt.Fprintf(w, "[%d]\n", i+1)
		_, _ = fmt.Fprintf(w, "sent_at: %s\n", record.SentAt.UTC().Format(time.RFC3339))
		_, _ = fmt.Fprintf(w, "to_peer_id: %s\n", record.ToPeerID)
		_, _ = fmt.Fprintf(w, "topic: %s\n", record.Topic)
		_, _ = fmt.Fprintf(w, "session_id: %s\n", record.SessionID)
		if strings.TrimSpace(record.ReplyTo) != "" {
			_, _ = fmt.Fprintf(w, "reply_to: %s\n", record.ReplyTo)
		}
		_, _ = fmt.Fprintf(w, "idempotency_key: %s\n", record.IdempotencyKey)
		_, _ = fmt.Fprintf(w, "content_type: %s\n", record.ContentType)
		_, _ = fmt.Fprintln(w, "payload:")
		_, _ = fmt.Fprintln(w, indentBlock(payloadSummary, "  "))
		if i < len(records)-1 {
			_, _ = fmt.Fprintln(w, "")
		}
	}
}

func indentBlock(text string, prefix string) string {
	prefix = strings.TrimRight(prefix, "\n")
	if prefix == "" {
		prefix = "  "
	}
	if strings.TrimSpace(text) == "" {
		return prefix + "(empty)"
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
