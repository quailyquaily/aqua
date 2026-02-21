package main

import (
	"fmt"
	"time"

	"github.com/quailyquaily/aqua/aqua"
	"github.com/spf13/cobra"
)

func newIDCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "id [nickname]",
		Short: "Show local Aqua identity or set nickname",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			now := time.Now().UTC()
			var identity aqua.Identity
			var err error
			if len(args) == 1 {
				if _, _, err := svc.EnsureIdentity(cmd.Context(), now); err != nil {
					return err
				}
				identity, err = svc.SetIdentityNickname(cmd.Context(), args[0], now)
				if err != nil {
					return err
				}
			} else {
				identity, _, err = svc.EnsureIdentity(cmd.Context(), now)
				if err != nil {
					return err
				}
			}

			fingerprint, _ := aqua.FingerprintGrouped(identity.IdentityPubEd25519)
			view := map[string]any{
				"node_uuid":            identity.NodeUUID,
				"peer_id":              identity.PeerID,
				"node_id":              identity.NodeID,
				"nickname":             identity.Nickname,
				"identity_pub_ed25519": identity.IdentityPubEd25519,
				"fingerprint":          fingerprint,
				"created_at":           identity.CreatedAt,
				"updated_at":           identity.UpdatedAt,
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), view)
			}

			_, _ = fmt.Fprintf(
				cmd.OutOrStdout(),
				"node_uuid: %s\npeer_id: %s\nnode_id: %s\nnickname: %s\nidentity_pub_ed25519: %s\nfingerprint: %s\ncreated_at: %s\nupdated_at: %s\n",
				identity.NodeUUID,
				identity.PeerID,
				identity.NodeID,
				identity.Nickname,
				identity.IdentityPubEd25519,
				fingerprint,
				identity.CreatedAt.UTC().Format(time.RFC3339),
				identity.UpdatedAt.UTC().Format(time.RFC3339),
			)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}
