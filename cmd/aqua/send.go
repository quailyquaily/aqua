package main

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/quailyquaily/aqua/aqua"
	"github.com/spf13/cobra"
)

func newSendCmd() *cobra.Command {
	var addresses []string
	var topic string
	var message string
	var contentType string
	var idempotencyKey string
	var sessionID string
	var replyTo string
	var notify bool
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "send <peer_id> [message]",
		Short: "Send agent.data.push to a contact",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedMessage, err := resolvePushMessage(message, args)
			if err != nil {
				return err
			}
			topic = strings.TrimSpace(topic)
			if topic == "" {
				return fmt.Errorf("--topic is required")
			}
			contentType = strings.TrimSpace(contentType)
			if contentType == "" {
				contentType = "application/json"
			}
			resolvedSessionID, err := resolveOrGenerateSessionID(sessionID)
			if err != nil {
				return err
			}
			resolvedReplyTo := strings.TrimSpace(replyTo)

			messageID := uuid.NewString()
			payloadBytes := []byte(resolvedMessage)
			idempotencyKey = strings.TrimSpace(idempotencyKey)
			if idempotencyKey == "" {
				idempotencyKey = messageEnvelopeKey(messageID)
			}

			req := aqua.DataPushRequest{
				Topic:          topic,
				ContentType:    contentType,
				PayloadBase64:  base64.RawURLEncoding.EncodeToString(payloadBytes),
				IdempotencyKey: idempotencyKey,
				SessionID:      resolvedSessionID,
				ReplyTo:        resolvedReplyTo,
			}

			node, err := newDialNode(cmd)
			if err != nil {
				return err
			}
			defer node.Close()

			result, err := node.PushData(cmd.Context(), args[0], addresses, req, notify)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"peer_id":         args[0],
					"topic":           req.Topic,
					"content_type":    req.ContentType,
					"idempotency_key": req.IdempotencyKey,
					"session_id":      resolvedSessionID,
					"reply_to":        resolvedReplyTo,
					"notification":    notify,
					"result":          result,
				})
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "peer_id: %s\ntopic: %s\ncontent_type: %s\nsession_id: %s\nreply_to: %s\nidempotency_key: %s\nnotification: %v\naccepted: %v\ndeduped: %v\n", args[0], req.Topic, req.ContentType, resolvedSessionID, resolvedReplyTo, req.IdempotencyKey, notify, result.Accepted, result.Deduped)
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&addresses, "address", nil, "Override dial address (repeatable)")
	cmd.Flags().StringVar(&topic, "topic", "chat.message", "Data topic")
	cmd.Flags().StringVar(&message, "message", "", "Message payload (optional; can also be provided as positional argument)")
	cmd.Flags().StringVar(&contentType, "content-type", "text/plain", "Content type")
	cmd.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "Idempotency key (default: derived from message_id)")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Session id (UUIDv7, auto-generated when omitted)")
	cmd.Flags().StringVar(&replyTo, "reply-to", "", "Reply target message id")
	cmd.Flags().BoolVar(&notify, "notify", false, "Send as JSON-RPC notification (no response expected)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func resolveOrGenerateSessionID(rawSessionID string) (string, error) {
	sessionID := strings.TrimSpace(rawSessionID)
	if sessionID == "" {
		generated, err := uuid.NewV7()
		if err != nil {
			return "", fmt.Errorf("generate session_id: %w", err)
		}
		return generated.String(), nil
	}

	parsedSession, err := uuid.Parse(sessionID)
	if err != nil || parsedSession.Version() != uuid.Version(7) {
		return "", fmt.Errorf("--session-id must be uuid_v7")
	}
	return sessionID, nil
}

func resolvePushMessage(rawMessage string, args []string) (string, error) {
	flagMessage := strings.TrimSpace(rawMessage)
	positionalMessage := ""
	if len(args) >= 2 {
		positionalMessage = strings.TrimSpace(args[1])
	}
	if positionalMessage != "" && flagMessage != "" {
		return "", fmt.Errorf("message provided by both positional argument and --message")
	}
	if positionalMessage != "" {
		return positionalMessage, nil
	}
	if flagMessage != "" {
		return flagMessage, nil
	}
	return "", fmt.Errorf("message is required (positional argument or --message)")
}
