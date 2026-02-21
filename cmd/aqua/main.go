package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/quailyquaily/aqua/maep"
	"github.com/spf13/cobra"
)

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aqua",
		Short: "Standalone MAEP program",
	}
	cmd.PersistentFlags().String("dir", defaultMAEPDir(), "MAEP state directory")

	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newIDCmd())
	cmd.AddCommand(newCardCmd())
	cmd.AddCommand(newContactsCmd())
	cmd.AddCommand(newAuditCmd())
	cmd.AddCommand(newInboxCmd())
	cmd.AddCommand(newOutboxCmd())
	cmd.AddCommand(newServeCmd())
	cmd.AddCommand(newHelloCmd())
	cmd.AddCommand(newPingCmd())
	cmd.AddCommand(newCapabilitiesCmd())
	cmd.AddCommand(newPushCmd())
	return cmd
}

func newInitCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize local MAEP identity",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			identity, created, err := svc.EnsureIdentity(cmd.Context(), time.Now().UTC())
			if err != nil {
				return err
			}
			fingerprint, _ := maep.FingerprintGrouped(identity.IdentityPubEd25519)
			view := map[string]any{
				"created":     created,
				"node_uuid":   identity.NodeUUID,
				"peer_id":     identity.PeerID,
				"node_id":     identity.NodeID,
				"fingerprint": fingerprint,
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), view)
			}

			state := "existing"
			if created {
				state = "created"
			}
			_, _ = fmt.Fprintf(
				cmd.OutOrStdout(),
				"state: %s\nnode_uuid: %s\npeer_id: %s\nnode_id: %s\nfingerprint: %s\n",
				state,
				identity.NodeUUID,
				identity.PeerID,
				identity.NodeID,
				fingerprint,
			)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newIDCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "id",
		Short: "Show local MAEP identity",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			identity, ok, err := svc.GetIdentity(cmd.Context())
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("identity not found; run `aqua init`")
			}

			fingerprint, _ := maep.FingerprintGrouped(identity.IdentityPubEd25519)
			view := map[string]any{
				"node_uuid":            identity.NodeUUID,
				"peer_id":              identity.PeerID,
				"node_id":              identity.NodeID,
				"identity_pub_ed25519": identity.IdentityPubEd25519,
				"fingerprint":          fingerprint,
				"created_at":           identity.CreatedAt,
				"updated_at":           identity.UpdatedAt,
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), view)
			}

			_, _ = fmt.Fprintf(
				cmd.OutOrStdout(),
				"node_uuid: %s\npeer_id: %s\nnode_id: %s\nidentity_pub_ed25519: %s\nfingerprint: %s\ncreated_at: %s\nupdated_at: %s\n",
				identity.NodeUUID,
				identity.PeerID,
				identity.NodeID,
				identity.IdentityPubEd25519,
				fingerprint,
				identity.CreatedAt.UTC().Format(time.RFC3339),
				identity.UpdatedAt.UTC().Format(time.RFC3339),
			)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newCardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "card",
		Short: "Manage MAEP contact cards",
	}
	cmd.AddCommand(newCardExportCmd())
	return cmd
}

func newCardExportCmd() *cobra.Command {
	var addresses []string
	var listenAddrs []string
	var outPath string
	var minProtocol int
	var maxProtocol int
	var expiresIn time.Duration

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export a signed contact card",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			resolvedAddresses, err := resolveCardExportAddressesForCommand(
				cmd.Context(),
				svc,
				addresses,
				listenAddrs,
				cmd.InOrStdin(),
				cmd.ErrOrStderr(),
			)
			if err != nil {
				return err
			}

			var expiresAt *time.Time
			if expiresIn > 0 {
				t := time.Now().UTC().Add(expiresIn)
				expiresAt = &t
			}

			_, raw, err := svc.ExportContactCard(cmd.Context(), resolvedAddresses, minProtocol, maxProtocol, time.Now().UTC(), expiresAt)
			if err != nil {
				return err
			}
			if strings.TrimSpace(outPath) == "" {
				_, _ = cmd.OutOrStdout().Write(raw)
				return nil
			}
			path := expandHomePath(strings.TrimSpace(outPath))
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "written: %s\n", path)
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&addresses, "address", nil, "Contact multiaddr (repeatable, must end with /p2p/<peer_id>)")
	cmd.Flags().StringArrayVar(&listenAddrs, "listen", nil, "Configured listen multiaddr (repeatable), used when --address is empty")
	cmd.Flags().StringVar(&outPath, "out", "", "Output file path (default stdout)")
	cmd.Flags().IntVar(&minProtocol, "min-protocol", 1, "Minimum supported protocol version")
	cmd.Flags().IntVar(&maxProtocol, "max-protocol", 1, "Maximum supported protocol version")
	cmd.Flags().DurationVar(&expiresIn, "expires-in", 0, "Relative expiration duration, e.g. 720h (0 disables)")

	return cmd
}

func newContactsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contacts",
		Short: "Manage MAEP contacts",
	}
	cmd.AddCommand(newContactsListCmd())
	cmd.AddCommand(newContactsImportCmd())
	cmd.AddCommand(newContactsShowCmd())
	cmd.AddCommand(newContactsVerifyCmd())
	return cmd
}

func newContactsListCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List contacts",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			items, err := svc.ListContacts(cmd.Context())
			if err != nil {
				return err
			}
			sort.Slice(items, func(i, j int) bool {
				if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
					return items[i].PeerID < items[j].PeerID
				}
				return items[i].UpdatedAt.After(items[j].UpdatedAt)
			})

			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), items)
			}
			if len(items) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no contacts")
				return nil
			}
			for _, c := range items {
				display := strings.TrimSpace(c.DisplayName)
				if display == "" {
					display = "-"
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", c.PeerID, c.TrustState, c.NodeUUID, display)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newContactsImportCmd() *cobra.Command {
	var displayName string
	cmd := &cobra.Command{
		Use:   "import <contact_card.json|->",
		Short: "Import a contact card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := readInputFile(args[0])
			if err != nil {
				return err
			}
			svc := serviceFromCmd(cmd)
			result, err := svc.ImportContactCard(cmd.Context(), raw, displayName, time.Now().UTC())
			if err != nil {
				symbol := maep.SymbolOf(err)
				if symbol != "" {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "symbol: %s\n", symbol)
				}
				return err
			}
			status := "updated"
			if result.Created {
				status = "created"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "status: %s\npeer_id: %s\nnode_uuid: %s\ntrust_state: %s\n", status, result.Contact.PeerID, result.Contact.NodeUUID, result.Contact.TrustState)
			return nil
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "Display name override for this contact")
	return cmd
}

func newContactsShowCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "show <peer_id>",
		Short: "Show contact detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			contact, ok, err := svc.GetContactByPeerID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("contact not found: %s", args[0])
			}

			fingerprint, _ := maep.FingerprintGrouped(contact.IdentityPubEd25519)
			view := map[string]any{
				"peer_id":                contact.PeerID,
				"node_uuid":              contact.NodeUUID,
				"node_id":                contact.NodeID,
				"display_name":           contact.DisplayName,
				"identity_pub_ed25519":   contact.IdentityPubEd25519,
				"fingerprint":            fingerprint,
				"addresses":              contact.Addresses,
				"trust_state":            contact.TrustState,
				"min_supported_protocol": contact.MinSupportedProtocol,
				"max_supported_protocol": contact.MaxSupportedProtocol,
				"issued_at":              contact.IssuedAt,
				"expires_at":             contact.ExpiresAt,
				"updated_at":             contact.UpdatedAt,
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), view)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "peer_id: %s\nnode_uuid: %s\nnode_id: %s\ndisplay_name: %s\ntrust_state: %s\nfingerprint: %s\n", contact.PeerID, contact.NodeUUID, contact.NodeID, contact.DisplayName, contact.TrustState, fingerprint)
			for _, addr := range contact.Addresses {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "address: %s\n", addr)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newContactsVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify <peer_id>",
		Short: "Mark contact as verified after out-of-band fingerprint check",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			contact, err := svc.MarkContactVerified(cmd.Context(), args[0], time.Now().UTC())
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "peer_id: %s\ntrust_state: %s\n", contact.PeerID, contact.TrustState)
			return nil
		},
	}
	return cmd
}

func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Query MAEP trust-state and operation audit events",
	}
	cmd.AddCommand(newAuditListCmd())
	return cmd
}

func newAuditListCmd() *cobra.Command {
	var peerID string
	var action string
	var limit int
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List audit events",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			events, err := svc.ListAuditEvents(cmd.Context(), peerID, action, limit)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), events)
			}
			if len(events) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no audit events")
				return nil
			}
			for _, event := range events {
				_, _ = fmt.Fprintf(
					cmd.OutOrStdout(),
					"%s\t%s\t%s\t%s\t%s\t%s\n",
					event.CreatedAt.UTC().Format(time.RFC3339),
					event.Action,
					event.PeerID,
					event.PreviousTrustState,
					event.NewTrustState,
					event.Reason,
				)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&peerID, "peer-id", "", "Filter by peer_id")
	cmd.Flags().StringVar(&action, "action", "", "Filter by action symbol")
	cmd.Flags().IntVar(&limit, "limit", 100, "Max number of records (<=0 means all)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newInboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "Query received agent.data.push messages",
	}
	cmd.AddCommand(newInboxListCmd())
	return cmd
}

func newInboxListCmd() *cobra.Command {
	var fromPeerID string
	var topic string
	var limit int
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List received messages from local inbox storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			records, err := svc.ListInboxMessages(cmd.Context(), fromPeerID, topic, limit)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), records)
			}
			if len(records) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no inbox messages")
				return nil
			}
			writeInboxRecords(cmd.OutOrStdout(), records)
			return nil
		},
	}
	cmd.Flags().StringVar(&fromPeerID, "from-peer-id", "", "Filter by sender peer_id")
	cmd.Flags().StringVar(&topic, "topic", "", "Filter by topic")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max number of records (<=0 means all)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newOutboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "outbox",
		Short: "Query sent agent.data.push messages",
	}
	cmd.AddCommand(newOutboxListCmd())
	return cmd
}

func newOutboxListCmd() *cobra.Command {
	var toPeerID string
	var topic string
	var limit int
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List sent messages from local outbox storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			records, err := svc.ListOutboxMessages(cmd.Context(), toPeerID, topic, limit)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), records)
			}
			if len(records) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no outbox messages")
				return nil
			}
			writeOutboxRecords(cmd.OutOrStdout(), records)
			return nil
		},
	}
	cmd.Flags().StringVar(&toPeerID, "to-peer-id", "", "Filter by destination peer_id")
	cmd.Flags().StringVar(&topic, "topic", "", "Filter by topic")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max number of records (<=0 means all)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newServeCmd() *cobra.Command {
	var listenAddrs []string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run MAEP libp2p node and handle incoming RPC streams",
		RunE: func(cmd *cobra.Command, args []string) error {
			runCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			svc := serviceFromCmd(cmd)
			identity, _, err := svc.EnsureIdentity(runCtx, time.Now().UTC())
			if err != nil {
				return err
			}
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: slog.LevelInfo}))
			node, err := maep.NewNode(runCtx, svc, maep.NodeOptions{
				ListenAddrs: listenAddrs,
				Logger:      logger,
				OnDataPush: func(event maep.DataPushEvent) {
					printDataPushEvent(cmd, event, outputJSON)
				},
			})
			if err != nil {
				return err
			}
			defer node.Close()

			if outputJSON {
				_ = writeJSON(cmd.OutOrStdout(), map[string]any{
					"status":    "ready",
					"peer_id":   node.PeerID(),
					"node_uuid": identity.NodeUUID,
					"addresses": node.AddrStrings(),
				})
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "status: ready\nnode_uuid: %s\npeer_id: %s\n", identity.NodeUUID, node.PeerID())
				for _, addr := range node.AddrStrings() {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "address: %s\n", addr)
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "waiting for incoming streams... (Ctrl+C to stop)")
			}

			<-runCtx.Done()
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&listenAddrs, "listen", nil, "Listen multiaddr (repeatable), default: /ip4/0.0.0.0/udp/0/quic-v1 and /ip4/0.0.0.0/tcp/0")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print status/events as JSON")
	return cmd
}

func newHelloCmd() *cobra.Command {
	var addresses []string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "hello <peer_id>",
		Short: "Run explicit hello negotiation with a contact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			node, err := newDialNode(cmd)
			if err != nil {
				return err
			}
			defer node.Close()
			result, err := node.DialHello(cmd.Context(), args[0], addresses)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "peer_id: %s\nremote_min: %d\nremote_max: %d\nnegotiated: %d\n", result.RemotePeerID, result.RemoteMinProtocol, result.RemoteMaxProtocol, result.NegotiatedProtocol)
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&addresses, "address", nil, "Override dial address (repeatable)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newPingCmd() *cobra.Command {
	var addresses []string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "ping <peer_id>",
		Short: "Send agent.ping RPC to a contact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			node, err := newDialNode(cmd)
			if err != nil {
				return err
			}
			defer node.Close()
			result, err := node.Ping(cmd.Context(), args[0], addresses)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "ok: %v\nts: %v\n", result["ok"], result["ts"])
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&addresses, "address", nil, "Override dial address (repeatable)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newCapabilitiesCmd() *cobra.Command {
	var addresses []string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "capabilities <peer_id>",
		Short: "Fetch remote capabilities via agent.capabilities.get",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			node, err := newDialNode(cmd)
			if err != nil {
				return err
			}
			defer node.Close()
			result, err := node.GetCapabilities(cmd.Context(), args[0], addresses)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "protocol_min: %v\nprotocol_max: %v\n", result["protocol_min"], result["protocol_max"])
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "capabilities: %v\n", result["capabilities"])
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "allowed_methods: %v\n", result["allowed_methods"])
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&addresses, "address", nil, "Override dial address (repeatable)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newPushCmd() *cobra.Command {
	var addresses []string
	var topic string
	var text string
	var contentType string
	var idempotencyKey string
	var sessionID string
	var replyTo string
	var notify bool
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "push <peer_id>",
		Short: "Send agent.data.push to a contact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text = strings.TrimSpace(text)
			if text == "" {
				return fmt.Errorf("--text is required")
			}
			topic = strings.TrimSpace(topic)
			if topic == "" {
				return fmt.Errorf("--topic is required")
			}
			contentType = strings.TrimSpace(contentType)
			if contentType == "" {
				contentType = "application/json"
			}
			if !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
				return fmt.Errorf("--content-type must be application/json envelope")
			}
			resolvedSessionID := strings.TrimSpace(sessionID)
			if resolvedSessionID != "" {
				parsedSession, err := uuid.Parse(resolvedSessionID)
				if err != nil || parsedSession.Version() != uuid.Version(7) {
					return fmt.Errorf("--session-id must be uuid_v7")
				}
			}
			if maep.IsDialogueTopic(topic) && resolvedSessionID == "" {
				return fmt.Errorf("--session-id is required for dialogue topics")
			}

			messageID := uuid.NewString()
			payload := map[string]any{
				"message_id": messageID,
				"text":       text,
				"sent_at":    time.Now().UTC().Format(time.RFC3339),
			}
			if resolvedSessionID != "" {
				payload["session_id"] = resolvedSessionID
			}
			if strings.TrimSpace(replyTo) != "" {
				payload["reply_to"] = strings.TrimSpace(replyTo)
			}
			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				return err
			}

			idempotencyKey = strings.TrimSpace(idempotencyKey)
			if idempotencyKey == "" {
				idempotencyKey = messageEnvelopeKey(messageID)
			}

			req := maep.DataPushRequest{
				Topic:          topic,
				ContentType:    contentType,
				PayloadBase64:  base64.RawURLEncoding.EncodeToString(payloadBytes),
				IdempotencyKey: idempotencyKey,
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
					"reply_to":        strings.TrimSpace(replyTo),
					"notification":    notify,
					"result":          result,
				})
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "peer_id: %s\ntopic: %s\ncontent_type: %s\nsession_id: %s\nreply_to: %s\nidempotency_key: %s\nnotification: %v\naccepted: %v\ndeduped: %v\n", args[0], req.Topic, req.ContentType, resolvedSessionID, strings.TrimSpace(replyTo), req.IdempotencyKey, notify, result.Accepted, result.Deduped)
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&addresses, "address", nil, "Override dial address (repeatable)")
	cmd.Flags().StringVar(&topic, "topic", "chat.message", "Data topic")
	cmd.Flags().StringVar(&text, "text", "", "Text payload")
	cmd.Flags().StringVar(&contentType, "content-type", "application/json", "Content type (must be application/json envelope)")
	cmd.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "Idempotency key (default: derived from message_id)")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Session id for JSON payload")
	cmd.Flags().StringVar(&replyTo, "reply-to", "", "Reply target message id for JSON payload")
	cmd.Flags().BoolVar(&notify, "notify", false, "Send as JSON-RPC notification (no response expected)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newDialNode(cmd *cobra.Command) (*maep.Node, error) {
	svc := serviceFromCmd(cmd)
	logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: slog.LevelInfo}))
	return maep.NewNode(cmd.Context(), svc, maep.NodeOptions{DialOnly: true, Logger: logger})
}

func printDataPushEvent(cmd *cobra.Command, event maep.DataPushEvent, outputJSON bool) {
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
			return "<invalid-json>"
		}
		if isMAEPEnvelopeJSON(obj) {
			text, err := json.MarshalIndent(obj, "", "  ")
			if err != nil {
				return "<json-encode-error>"
			}
			return string(text)
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

func writeInboxRecords(w io.Writer, records []maep.InboxMessage) {
	for i, record := range records {
		payloadSummary := summarizePayload(record.ContentType, record.PayloadBase64)
		_, _ = fmt.Fprintf(w, "[%d]\n", i+1)
		_, _ = fmt.Fprintf(w, "received_at: %s\n", record.ReceivedAt.UTC().Format(time.RFC3339))
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

func writeOutboxRecords(w io.Writer, records []maep.OutboxMessage) {
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

func isMAEPEnvelopeJSON(v any) bool {
	obj, ok := v.(map[string]any)
	if !ok {
		return false
	}
	if _, ok := obj["message_id"].(string); !ok {
		return false
	}
	if _, ok := obj["text"].(string); !ok {
		return false
	}
	if _, ok := obj["sent_at"].(string); !ok {
		return false
	}
	return true
}

func resolveCardExportAddressesForCommand(
	ctx context.Context,
	svc *maep.Service,
	explicit []string,
	configuredListenAddrs []string,
	in io.Reader,
	out io.Writer,
) ([]string, error) {
	return resolveCardExportAddressesWithPrompt(ctx, svc, explicit, configuredListenAddrs, in, out, isInteractiveTerminal(in, out))
}

func resolveCardExportAddressesWithPrompt(
	ctx context.Context,
	svc *maep.Service,
	explicit []string,
	configuredListenAddrs []string,
	in io.Reader,
	out io.Writer,
	interactive bool,
) ([]string, error) {
	normalizedExplicit := normalizeAddressList(explicit)
	if len(normalizedExplicit) > 0 {
		return normalizedExplicit, nil
	}
	normalizedConfigured := normalizeAddressList(configuredListenAddrs)
	if len(normalizedConfigured) == 0 {
		return nil, fmt.Errorf("at least one --address is required (or set --listen)")
	}

	identity, ok, err := svc.GetIdentity(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("identity not found; run `aqua init`")
	}
	resolved, err := appendPeerIDToAddresses(normalizedConfigured, identity.PeerID)
	if err != nil {
		return nil, err
	}
	resolved = expandConfiguredDialAddresses(resolved)
	return selectCardExportDialAddresses(resolved, in, out, interactive)
}

func appendPeerIDToAddresses(addresses []string, peerID string) ([]string, error) {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return nil, fmt.Errorf("empty local peer_id")
	}
	peerComponent, err := ma.NewMultiaddr("/p2p/" + peerID)
	if err != nil {
		return nil, fmt.Errorf("build /p2p component: %w", err)
	}

	out := make([]string, 0, len(addresses))
	seen := map[string]bool{}
	for _, raw := range addresses {
		addr := strings.TrimSpace(raw)
		if addr == "" {
			continue
		}
		maddr, err := ma.NewMultiaddr(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid configured address %q: %w", addr, err)
		}
		if _, last := ma.SplitLast(maddr); last != nil && last.Protocol().Code == ma.P_P2P {
			if strings.TrimSpace(last.Value()) != peerID {
				return nil, fmt.Errorf("configured address %q has mismatched /p2p/%s (local peer_id=%s)", addr, last.Value(), peerID)
			}
			canonical := maddr.String()
			if !seen[canonical] {
				seen[canonical] = true
				out = append(out, canonical)
			}
			continue
		}

		withPeer := maddr.Encapsulate(peerComponent).String()
		if seen[withPeer] {
			continue
		}
		seen[withPeer] = true
		out = append(out, withPeer)
	}
	return out, nil
}

func expandConfiguredDialAddresses(addresses []string) []string {
	localIPs, err := discoverLocalInterfaceIPs()
	if err != nil || len(localIPs) == 0 {
		return normalizeAddressList(addresses)
	}
	return expandConfiguredDialAddressesWithIPs(addresses, localIPs)
}

func expandConfiguredDialAddressesWithIPs(addresses []string, localIPs []net.IP) []string {
	out := make([]string, 0, len(addresses))
	seen := map[string]bool{}
	add := func(addr string) {
		addr = strings.TrimSpace(addr)
		if addr == "" || seen[addr] {
			return
		}
		seen[addr] = true
		out = append(out, addr)
	}

	for _, raw := range normalizeAddressList(addresses) {
		add(raw)

		maddr, err := ma.NewMultiaddr(raw)
		if err != nil {
			continue
		}
		if value, err := maddr.ValueForProtocol(ma.P_IP4); err == nil {
			if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil && ip.IsUnspecified() {
				for _, local := range localIPs {
					v4 := local.To4()
					if v4 == nil {
						continue
					}
					replaced, err := replaceMultiaddrIPComponent(maddr, ma.P_IP4, v4.String())
					if err != nil {
						continue
					}
					add(replaced.String())
				}
			}
		}
		if value, err := maddr.ValueForProtocol(ma.P_IP6); err == nil {
			if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil && ip.IsUnspecified() {
				for _, local := range localIPs {
					if local.To4() != nil || local.To16() == nil {
						continue
					}
					replaced, err := replaceMultiaddrIPComponent(maddr, ma.P_IP6, local.String())
					if err != nil {
						continue
					}
					add(replaced.String())
				}
			}
		}
	}
	return out
}

func replaceMultiaddrIPComponent(addr ma.Multiaddr, protoCode int, value string) (ma.Multiaddr, error) {
	if addr == nil {
		return nil, fmt.Errorf("nil multiaddr")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return addr, nil
	}
	raw := addr.String()
	updated := raw
	switch protoCode {
	case ma.P_IP4:
		if strings.Contains(raw, "/ip4/0.0.0.0/") {
			updated = strings.Replace(raw, "/ip4/0.0.0.0/", "/ip4/"+value+"/", 1)
		} else if strings.HasSuffix(raw, "/ip4/0.0.0.0") {
			updated = strings.TrimSuffix(raw, "/ip4/0.0.0.0") + "/ip4/" + value
		}
	case ma.P_IP6:
		if strings.Contains(raw, "/ip6/::/") {
			updated = strings.Replace(raw, "/ip6/::/", "/ip6/"+value+"/", 1)
		} else if strings.HasSuffix(raw, "/ip6/::") {
			updated = strings.TrimSuffix(raw, "/ip6/::") + "/ip6/" + value
		}
	default:
		return addr, nil
	}
	if updated == raw {
		return addr, nil
	}
	rebuilt, err := ma.NewMultiaddr(updated)
	if err != nil {
		return nil, err
	}
	return rebuilt, nil
}

func discoverLocalInterfaceIPs() ([]net.IP, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, 8)
	seen := map[string]bool{}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := interfaceAddrIP(addr)
			if ip == nil || ip.IsUnspecified() {
				continue
			}
			key := ip.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ip)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsLoopback() != out[j].IsLoopback() {
			return !out[i].IsLoopback()
		}
		isV4i := out[i].To4() != nil
		isV4j := out[j].To4() != nil
		if isV4i != isV4j {
			return isV4i
		}
		return out[i].String() < out[j].String()
	})
	return out, nil
}

func interfaceAddrIP(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		if v == nil || v.IP == nil {
			return nil
		}
		return v.IP
	case *net.IPAddr:
		if v == nil || v.IP == nil {
			return nil
		}
		return v.IP
	default:
		return nil
	}
}

func selectCardExportDialAddresses(addresses []string, in io.Reader, out io.Writer, interactive bool) ([]string, error) {
	valid, invalid := classifyCardExportDialAddresses(addresses)
	if len(valid) == 0 {
		if len(invalid) == 0 {
			return nil, fmt.Errorf("no dialable addresses available (provide --address explicitly)")
		}
		reasons := make([]string, 0, len(invalid))
		for _, item := range invalid {
			reasons = append(reasons, fmt.Sprintf("%s (%s)", item.Address, item.Reason))
		}
		return nil, fmt.Errorf("no dialable addresses available from --listen: %s", strings.Join(reasons, "; "))
	}
	if !interactive {
		if len(valid) == 1 {
			return []string{valid[0]}, nil
		}
		return nil, fmt.Errorf("multiple dialable addresses found; pass --address explicitly or run in an interactive terminal")
	}

	if out == nil {
		out = os.Stderr
	}
	if in == nil {
		in = os.Stdin
	}
	_, _ = fmt.Fprintln(out, "No --address provided. Select one dialable address for contact card:")
	for i, addr := range valid {
		_, _ = fmt.Fprintf(out, "  %d) %s\n", i+1, addr)
	}
	if len(invalid) > 0 {
		_, _ = fmt.Fprintln(out, "Ignored non-dialable addresses:")
		for _, item := range invalid {
			_, _ = fmt.Fprintf(out, "  - %s (%s)\n", item.Address, item.Reason)
		}
	}

	reader := bufio.NewReader(in)
	for {
		_, _ = fmt.Fprintf(out, "Select address [1-%d] (default 1): ", len(valid))
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("read selection: %w", err)
		}
		choice := strings.TrimSpace(line)
		if choice == "" {
			return []string{valid[0]}, nil
		}
		if strings.EqualFold(choice, "q") || strings.EqualFold(choice, "quit") {
			return nil, fmt.Errorf("card export cancelled")
		}
		index, parseErr := strconv.Atoi(choice)
		if parseErr == nil && index >= 1 && index <= len(valid) {
			return []string{valid[index-1]}, nil
		}
		_, _ = fmt.Fprintf(out, "Invalid selection: %q\n", choice)
		if err == io.EOF {
			return nil, fmt.Errorf("invalid selection: %q", choice)
		}
	}
}

type invalidDialAddress struct {
	Address string
	Reason  string
}

func classifyCardExportDialAddresses(addresses []string) ([]string, []invalidDialAddress) {
	valid := make([]string, 0, len(addresses))
	invalid := make([]invalidDialAddress, 0, len(addresses))
	for _, raw := range addresses {
		address := strings.TrimSpace(raw)
		if address == "" {
			continue
		}
		reason := dialAddressInvalidReason(address)
		if reason != "" {
			invalid = append(invalid, invalidDialAddress{Address: address, Reason: reason})
			continue
		}
		valid = append(valid, address)
	}
	return valid, invalid
}

func dialAddressInvalidReason(address string) string {
	maddr, err := ma.NewMultiaddr(address)
	if err != nil {
		return "invalid multiaddr"
	}
	if value, err := maddr.ValueForProtocol(ma.P_IP4); err == nil {
		if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil && ip.IsUnspecified() {
			return "ip4 unspecified"
		}
	}
	if value, err := maddr.ValueForProtocol(ma.P_IP6); err == nil {
		if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil && ip.IsUnspecified() {
			return "ip6 unspecified"
		}
	}
	if value, err := maddr.ValueForProtocol(ma.P_TCP); err == nil && strings.TrimSpace(value) == "0" {
		return "tcp port 0"
	}
	if value, err := maddr.ValueForProtocol(ma.P_UDP); err == nil && strings.TrimSpace(value) == "0" {
		return "udp port 0"
	}
	return ""
}

func isInteractiveTerminal(in io.Reader, out io.Writer) bool {
	inFile, ok := in.(*os.File)
	if !ok {
		return false
	}
	outFile, ok := out.(*os.File)
	if !ok {
		return false
	}
	inInfo, err := inFile.Stat()
	if err != nil {
		return false
	}
	outInfo, err := outFile.Stat()
	if err != nil {
		return false
	}
	return (inInfo.Mode()&os.ModeCharDevice) != 0 && (outInfo.Mode()&os.ModeCharDevice) != 0
}

func normalizeAddressList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func serviceFromCmd(cmd *cobra.Command) *maep.Service {
	dir, _ := cmd.Flags().GetString("dir")
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = defaultMAEPDir()
	}
	dir = expandHomePath(dir)
	store := maep.NewFileStore(dir)
	return maep.NewService(store)
}

func defaultMAEPDir() string {
	if v := strings.TrimSpace(os.Getenv("AQUA_MAEP_DIR")); v != "" {
		return expandHomePath(v)
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ".aqua/maep"
	}
	return filepath.Join(home, ".aqua", "maep")
}

func expandHomePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

func readInputFile(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func messageEnvelopeKey(messageID string) string {
	return "msg:" + idempotencyToken(messageID)
}

func idempotencyToken(input string) string {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(input))
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
