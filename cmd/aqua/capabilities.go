package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCapabilitiesCmd() *cobra.Command {
	var addresses []string
	var relayMode string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "capabilities <peer_id>",
		Short: "Fetch remote capabilities via agent.capabilities.get",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			node, err := newDialNodeWithRelayMode(cmd, relayMode)
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
	cmd.Flags().StringVar(&relayMode, "relay-mode", "auto", "Relay dial mode: auto|off|required")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}
