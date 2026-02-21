package main

import "github.com/spf13/cobra"

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aqua",
		Short: "Standalone Aqua program",
	}
	cmd.PersistentFlags().String("dir", defaultAquaDir(), "Aqua state directory")

	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newIDCmd())
	cmd.AddCommand(newCardCmd())
	cmd.AddCommand(newContactsCmd())
	cmd.AddCommand(newInboxCmd())
	cmd.AddCommand(newOutboxCmd())
	cmd.AddCommand(newServeCmd())
	cmd.AddCommand(newHelloCmd())
	cmd.AddCommand(newPingCmd())
	cmd.AddCommand(newCapabilitiesCmd())
	cmd.AddCommand(newSendCmd())
	cmd.AddCommand(newVersionCmd())
	return cmd
}
