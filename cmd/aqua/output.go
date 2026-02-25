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

type inboxMessageJSON struct {
	aqua.InboxMessage
	PayloadText *string `json:"payload_text,omitempty"`
}

type outboxMessageJSON struct {
	aqua.OutboxMessage
	PayloadText *string `json:"payload_text,omitempty"`
}

func printDataPushEvent(cmd *cobra.Command, event aqua.DataPushEvent, outputJSON bool) {
	if outputJSON {
		payloadText := ""
		if payloadIsTextual(event.ContentType) {
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
	if payloadIsJSON(event.ContentType) {
		var obj any
		if err := json.Unmarshal(event.PayloadBytes, &obj); err == nil {
			pretty, _ := json.MarshalIndent(obj, "", "  ")
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "payload(json): %s\n", string(pretty))
			return
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "payload(text): %s\n", string(event.PayloadBytes))
		return
	}
	if payloadIsTextual(event.ContentType) {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "payload(text): %s\n", string(event.PayloadBytes))
		return
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "payload(bytes): %d\n", len(event.PayloadBytes))
}

func printRelayEvent(cmd *cobra.Command, event aqua.RelayEvent, outputJSON bool) {
	if outputJSON {
		_ = writeJSON(cmd.OutOrStdout(), map[string]any{
			"event":          event.Event,
			"path":           event.Path,
			"relay_peer_id":  event.RelayPeerID,
			"target_peer_id": event.TargetPeerID,
			"reason":         event.Reason,
			"timestamp":      event.Timestamp,
		})
		return
	}

	_, _ = fmt.Fprintf(
		cmd.OutOrStdout(),
		"relay_event: event=%s path=%s relay_peer_id=%s target_peer_id=%s reason=%s at=%s\n",
		event.Event,
		event.Path,
		event.RelayPeerID,
		event.TargetPeerID,
		event.Reason,
		event.Timestamp.UTC().Format(time.RFC3339),
	)
}

func summarizePayload(contentType string, payloadBase64 string) string {
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(payloadBase64))
	if err != nil {
		return "<invalid-base64>"
	}
	if payloadIsJSON(contentType) {
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
	if payloadIsTextual(contentType) {
		return string(data)
	}
	return fmt.Sprintf("<%d bytes>", len(data))
}

func inboxRecordsForJSON(records []aqua.InboxMessage) []inboxMessageJSON {
	if len(records) == 0 {
		return nil
	}
	out := make([]inboxMessageJSON, 0, len(records))
	for _, record := range records {
		item := inboxMessageJSON{InboxMessage: record}
		if text, ok := decodePayloadText(record.ContentType, record.PayloadBase64); ok {
			item.PayloadText = &text
		}
		out = append(out, item)
	}
	return out
}

func outboxRecordsForJSON(records []aqua.OutboxMessage) []outboxMessageJSON {
	if len(records) == 0 {
		return nil
	}
	out := make([]outboxMessageJSON, 0, len(records))
	for _, record := range records {
		item := outboxMessageJSON{OutboxMessage: record}
		if text, ok := decodePayloadText(record.ContentType, record.PayloadBase64); ok {
			item.PayloadText = &text
		}
		out = append(out, item)
	}
	return out
}

func decodePayloadText(contentType string, payloadBase64 string) (string, bool) {
	if !payloadIsTextual(contentType) {
		return "", false
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(payloadBase64))
	if err != nil {
		return "", false
	}
	return string(data), true
}

func payloadIsJSON(contentType string) bool {
	mediaType := normalizeContentType(contentType)
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func payloadIsTextual(contentType string) bool {
	mediaType := normalizeContentType(contentType)
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	return payloadIsJSON(mediaType)
}

func normalizeContentType(contentType string) string {
	lower := strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.Index(lower, ";"); idx >= 0 {
		lower = strings.TrimSpace(lower[:idx])
	}
	return lower
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
