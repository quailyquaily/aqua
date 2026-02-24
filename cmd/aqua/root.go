package main

import "github.com/spf13/cobra"

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aqua",
		Short: "Standalone Aqua program",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			_, err := resolveLogLevel(cmd)
			return err
		},
	}
	cmd.PersistentFlags().String("dir", defaultAquaDir(), "Aqua state directory")
	cmd.PersistentFlags().String("log-level", "info", "Log level: debug|info|warn|error")

	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newIDCmd())
	cmd.AddCommand(newCardCmd())
	cmd.AddCommand(newContactsCmd())
	cmd.AddCommand(newInboxCmd())
	cmd.AddCommand(newOutboxCmd())
	cmd.AddCommand(newServeCmd())
	cmd.AddCommand(newRelayCmd())
	cmd.AddCommand(newHelloCmd())
	cmd.AddCommand(newPingCmd())
	cmd.AddCommand(newCapabilitiesCmd())
	cmd.AddCommand(newSendCmd())
	cmd.AddCommand(newGroupCmd())
	cmd.AddCommand(newVersionCmd())
	return cmd
}
