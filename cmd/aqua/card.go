package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newCardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "card",
		Short: "Manage Aqua contact cards",
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
