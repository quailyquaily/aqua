package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

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
