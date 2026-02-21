package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

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
