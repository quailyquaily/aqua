package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/quailyquaily/aqua/aqua"
	"github.com/spf13/cobra"
)

func newInboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "Query received agent.data.push messages",
	}
	cmd.AddCommand(newInboxListCmd())
	cmd.AddCommand(newInboxMarkReadCmd())
	return cmd
}

func newInboxListCmd() *cobra.Command {
	var fromPeerID string
	var topic string
	var limit int
	var unreadOnly bool
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List received messages from local inbox storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			queryLimit := limit
			if unreadOnly {
				queryLimit = 0
			}
			records, err := svc.ListInboxMessages(cmd.Context(), fromPeerID, topic, queryLimit)
			if err != nil {
				return err
			}
			if unreadOnly {
				records = filterUnreadInbox(records, limit)
				unreadIDs := collectUnreadMessageIDs(records)
				if len(unreadIDs) > 0 {
					if _, err := svc.MarkInboxMessagesRead(cmd.Context(), unreadIDs, time.Now().UTC()); err != nil {
						return err
					}
				}
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), inboxRecordsForJSON(records))
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
	cmd.Flags().BoolVar(&unreadOnly, "unread", false, "Show unread messages only")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newInboxMarkReadCmd() *cobra.Command {
	var all bool
	var fromPeerID string
	var topic string
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "mark-read [message_id ...]",
		Short: "Mark inbox messages as read",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)

			var ids []string
			if all {
				if len(args) > 0 {
					return fmt.Errorf("cannot combine --all with message_id arguments")
				}
				records, err := svc.ListInboxMessages(cmd.Context(), fromPeerID, topic, 0)
				if err != nil {
					return err
				}
				ids = collectUnreadMessageIDs(records)
			} else {
				ids = normalizeInboxMessageIDs(args)
				if len(ids) == 0 {
					return fmt.Errorf("provide at least one message_id or use --all")
				}
			}

			marked, err := svc.MarkInboxMessagesRead(cmd.Context(), ids, time.Now().UTC())
			if err != nil {
				return err
			}

			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"all":          all,
					"from_peer_id": strings.TrimSpace(fromPeerID),
					"topic":        strings.TrimSpace(topic),
					"requested":    len(ids),
					"marked":       marked,
				})
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "requested: %d\nmarked: %d\n", len(ids), marked)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Mark all matching messages as read")
	cmd.Flags().StringVar(&fromPeerID, "from-peer-id", "", "Filter by sender peer_id (used with --all)")
	cmd.Flags().StringVar(&topic, "topic", "", "Filter by topic (used with --all)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func filterUnreadInbox(records []aqua.InboxMessage, limit int) []aqua.InboxMessage {
	if len(records) == 0 {
		return nil
	}
	out := make([]aqua.InboxMessage, 0, len(records))
	for _, record := range records {
		if record.Read {
			continue
		}
		out = append(out, record)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func collectUnreadMessageIDs(records []aqua.InboxMessage) []string {
	if len(records) == 0 {
		return nil
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		if record.Read {
			continue
		}
		id := strings.TrimSpace(record.MessageID)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	return normalizeInboxMessageIDs(ids)
}

func normalizeInboxMessageIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
