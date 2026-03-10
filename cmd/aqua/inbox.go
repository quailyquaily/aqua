package main

import (
	"context"
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
	cmd.AddCommand(newInboxWatchCmd())
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

func newInboxWatchCmd() *cobra.Command {
	var fromPeerID string
	var topic string
	var limit int
	var pollInterval time.Duration
	var batchWindow time.Duration
	var timeout time.Duration
	var markRead bool
	var once bool
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch unread inbox messages for agent processing",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			watchCtx := cmd.Context()
			cancel := func() {}
			if timeout > 0 {
				watchCtx, cancel = context.WithTimeout(watchCtx, timeout)
			}
			defer cancel()

			seen := map[string]bool{}
			printedAny := false
			for {
				records, err := waitForUnreadBatch(watchCtx, svc, fromPeerID, topic, limit, pollInterval, batchWindow, seen)
				if err != nil {
					if err == context.Canceled || err == context.DeadlineExceeded {
						return nil
					}
					return err
				}
				if len(records) == 0 {
					continue
				}
				if outputJSON {
					if err := writeJSON(cmd.OutOrStdout(), inboxRecordsForJSON(records)); err != nil {
						return err
					}
				} else {
					if printedAny {
						_, _ = fmt.Fprintln(cmd.OutOrStdout(), "")
					}
					writeInboxRecords(cmd.OutOrStdout(), records)
				}
				printedAny = true
				if markRead {
					if _, err := svc.MarkInboxMessagesRead(cmd.Context(), collectUnreadMessageIDs(records), time.Now().UTC()); err != nil {
						return err
					}
				}
				if once {
					return nil
				}
			}
		},
	}
	cmd.Flags().StringVar(&fromPeerID, "from-peer-id", "", "Filter by sender peer_id")
	cmd.Flags().StringVar(&topic, "topic", "", "Filter by topic")
	cmd.Flags().IntVar(&limit, "limit", 0, "Max number of records per wake (<=0 means all)")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 250*time.Millisecond, "Inbox poll interval while waiting")
	cmd.Flags().DurationVar(&batchWindow, "batch-window", 0, "Optional coalescing window after first unread message arrives")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Stop watching after this duration (default: no timeout)")
	cmd.Flags().BoolVar(&markRead, "mark-read", false, "Mark delivered records as read after printing them")
	cmd.Flags().BoolVar(&once, "once", false, "Exit after the first matching batch is printed")
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

func waitForUnreadBatch(ctx context.Context, svc *aqua.Service, fromPeerID string, topic string, limit int, pollInterval time.Duration, batchWindow time.Duration, seen map[string]bool) ([]aqua.InboxMessage, error) {
	if pollInterval <= 0 {
		pollInterval = 250 * time.Millisecond
	}
	if batchWindow < 0 {
		batchWindow = 0
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		batch, err := listNewUnreadMessages(ctx, svc, fromPeerID, topic, limit, seen)
		if err != nil {
			return nil, err
		}
		if len(batch) > 0 {
			if batchWindow > 0 && (limit <= 0 || len(batch) < limit) {
				batch, err = extendUnreadBatch(ctx, svc, fromPeerID, topic, limit, pollInterval, batchWindow, seen, batch)
				if err != nil {
					return nil, err
				}
			}
			return batch, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func extendUnreadBatch(ctx context.Context, svc *aqua.Service, fromPeerID string, topic string, limit int, pollInterval time.Duration, batchWindow time.Duration, seen map[string]bool, batch []aqua.InboxMessage) ([]aqua.InboxMessage, error) {
	deadline := time.Now().UTC().Add(batchWindow)
	for {
		if limit > 0 && len(batch) >= limit {
			return batch, nil
		}

		wait := minDuration(pollInterval, time.Until(deadline))
		if wait <= 0 {
			return batch, nil
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return batch, nil
		case <-timer.C:
		}

		remaining := 0
		if limit > 0 {
			remaining = limit - len(batch)
			if remaining <= 0 {
				return batch, nil
			}
		}
		more, err := listNewUnreadMessages(ctx, svc, fromPeerID, topic, remaining, seen)
		if err != nil {
			return nil, err
		}
		if len(more) > 0 {
			batch = append(batch, more...)
		}
	}
}

func listNewUnreadMessages(ctx context.Context, svc *aqua.Service, fromPeerID string, topic string, limit int, seen map[string]bool) ([]aqua.InboxMessage, error) {
	records, err := svc.ListInboxMessages(ctx, fromPeerID, topic, 0)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}

	out := make([]aqua.InboxMessage, 0, len(records))
	for _, record := range records {
		if record.Read {
			continue
		}
		messageID := strings.TrimSpace(record.MessageID)
		if messageID == "" || seen[messageID] {
			continue
		}
		seen[messageID] = true
		out = append(out, record)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func minDuration(a time.Duration, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
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
