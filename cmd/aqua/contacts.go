package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/quailyquaily/aqua/aqua"
	"github.com/spf13/cobra"
)

func newContactsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contacts",
		Short: "Manage Aqua contacts",
	}
	cmd.AddCommand(newContactsListCmd())
	cmd.AddCommand(newContactsAddCmd())
	cmd.AddCommand(newContactsDelCmd())
	cmd.AddCommand(newContactsImportCmd())
	cmd.AddCommand(newContactsShowCmd())
	cmd.AddCommand(newContactsVerifyCmd())
	return cmd
}

func newContactsListCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List contacts",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			items, err := svc.ListContacts(cmd.Context())
			if err != nil {
				return err
			}
			sort.Slice(items, func(i, j int) bool {
				if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
					return items[i].PeerID < items[j].PeerID
				}
				return items[i].UpdatedAt.After(items[j].UpdatedAt)
			})

			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), items)
			}
			if len(items) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no contacts")
				return nil
			}
			for _, c := range items {
				display := contactDisplayLabel(c)
				if display == "" {
					display = "-"
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", c.PeerID, c.TrustState, c.NodeUUID, display)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newContactsImportCmd() *cobra.Command {
	var displayName string
	cmd := &cobra.Command{
		Use:   "import <contact_card.json|->",
		Short: "Import a contact card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := readInputFile(args[0])
			if err != nil {
				return err
			}
			svc := serviceFromCmd(cmd)
			result, err := svc.ImportContactCard(cmd.Context(), raw, displayName, time.Now().UTC())
			if err != nil {
				symbol := aqua.SymbolOf(err)
				if symbol != "" {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "symbol: %s\n", symbol)
				}
				return err
			}
			status := "updated"
			if result.Created {
				status = "created"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "status: %s\npeer_id: %s\nnode_uuid: %s\ntrust_state: %s\n", status, result.Contact.PeerID, result.Contact.NodeUUID, result.Contact.TrustState)
			return nil
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "Display name override for this contact")
	return cmd
}

func newContactsAddCmd() *cobra.Command {
	var displayName string
	var verify bool
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "add <address>",
		Short: "Fetch and import a contact card from a peer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dialAddress := strings.TrimSpace(args[0])
			peerID, err := extractPeerIDFromDialAddress(dialAddress)
			if err != nil {
				return err
			}

			node, err := newDialNode(cmd)
			if err != nil {
				return err
			}
			defer node.Close()

			rawCard, err := node.GetContactCard(cmd.Context(), peerID, []string{dialAddress})
			if err != nil {
				symbol := aqua.SymbolOf(err)
				if symbol != "" {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "symbol: %s\n", symbol)
				}
				return err
			}

			svc := serviceFromCmd(cmd)
			result, err := svc.ImportContactCard(cmd.Context(), rawCard, displayName, time.Now().UTC())
			if err != nil {
				symbol := aqua.SymbolOf(err)
				if symbol != "" {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "symbol: %s\n", symbol)
				}
				return err
			}

			status := "updated"
			if result.Created {
				status = "created"
			}
			contact := result.Contact
			if verify {
				verifiedContact, err := svc.MarkContactVerified(cmd.Context(), contact.PeerID, time.Now().UTC())
				if err != nil {
					symbol := aqua.SymbolOf(err)
					if symbol != "" {
						_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "symbol: %s\n", symbol)
					}
					return err
				}
				contact = verifiedContact
			}

			fingerprint, _ := aqua.FingerprintGrouped(contact.IdentityPubEd25519)
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"status":      status,
					"peer_id":     contact.PeerID,
					"node_uuid":   contact.NodeUUID,
					"nickname":    contact.Nickname,
					"trust_state": contact.TrustState,
					"verified":    verify,
					"fingerprint": fingerprint,
				})
			}

			_, _ = fmt.Fprintf(
				cmd.OutOrStdout(),
				"status: %s\npeer_id: %s\nnode_uuid: %s\ntrust_state: %s\nfingerprint: %s\n",
				status,
				contact.PeerID,
				contact.NodeUUID,
				contact.TrustState,
				fingerprint,
			)
			return nil
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "Display name override for this contact")
	cmd.Flags().BoolVar(&verify, "verify", false, "Mark imported contact as verified")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newContactsDelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "del <peer_id>",
		Short: "Delete a contact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			if err := svc.DeleteContact(cmd.Context(), args[0], time.Now().UTC()); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "deleted: %s\n", strings.TrimSpace(args[0]))
			return nil
		},
	}
	return cmd
}

func newContactsShowCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "show <peer_id>",
		Short: "Show contact detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			contact, ok, err := svc.GetContactByPeerID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("contact not found: %s", args[0])
			}

			fingerprint, _ := aqua.FingerprintGrouped(contact.IdentityPubEd25519)
			view := map[string]any{
				"peer_id":                contact.PeerID,
				"node_uuid":              contact.NodeUUID,
				"node_id":                contact.NodeID,
				"display_name":           contact.DisplayName,
				"nickname":               contact.Nickname,
				"identity_pub_ed25519":   contact.IdentityPubEd25519,
				"fingerprint":            fingerprint,
				"addresses":              contact.Addresses,
				"trust_state":            contact.TrustState,
				"min_supported_protocol": contact.MinSupportedProtocol,
				"max_supported_protocol": contact.MaxSupportedProtocol,
				"issued_at":              contact.IssuedAt,
				"expires_at":             contact.ExpiresAt,
				"updated_at":             contact.UpdatedAt,
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), view)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "peer_id: %s\nnode_uuid: %s\nnode_id: %s\ndisplay_name: %s\nnickname: %s\ntrust_state: %s\nfingerprint: %s\n", contact.PeerID, contact.NodeUUID, contact.NodeID, contact.DisplayName, contact.Nickname, contact.TrustState, fingerprint)
			for _, addr := range contact.Addresses {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "address: %s\n", addr)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newContactsVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify <peer_id>",
		Short: "Mark contact as verified after out-of-band fingerprint check",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			contact, err := svc.MarkContactVerified(cmd.Context(), args[0], time.Now().UTC())
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "peer_id: %s\ntrust_state: %s\n", contact.PeerID, contact.TrustState)
			return nil
		},
	}
	return cmd
}
