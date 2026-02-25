package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

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
				return writeJSON(cmd.OutOrStdout(), outboxRecordsForJSON(records))
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
