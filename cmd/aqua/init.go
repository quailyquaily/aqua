package main

import (
	"fmt"
	"time"

	"github.com/quailyquaily/aqua/aqua"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize local Aqua identity",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			identity, created, err := svc.EnsureIdentity(cmd.Context(), time.Now().UTC())
			if err != nil {
				return err
			}
			fingerprint, _ := aqua.FingerprintGrouped(identity.IdentityPubEd25519)
			view := map[string]any{
				"created":     created,
				"node_uuid":   identity.NodeUUID,
				"peer_id":     identity.PeerID,
				"node_id":     identity.NodeID,
				"fingerprint": fingerprint,
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), view)
			}

			state := "existing"
			if created {
				state = "created"
			}
			_, _ = fmt.Fprintf(
				cmd.OutOrStdout(),
				"state: %s\nnode_uuid: %s\npeer_id: %s\nnode_id: %s\nfingerprint: %s\n",
				state,
				identity.NodeUUID,
				identity.PeerID,
				identity.NodeID,
				fingerprint,
			)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}
