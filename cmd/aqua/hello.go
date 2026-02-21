package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

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
