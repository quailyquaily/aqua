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
	cmd.AddCommand(newContactsInvitesCmd())
	cmd.AddCommand(newContactsInviteCmd())
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

func newContactsInvitesCmd() *cobra.Command {
	var outputJSON bool
	var status string
	var incomingOnly bool
	var all bool

	cmd := &cobra.Command{
		Use:     "invites",
		Aliases: []string{"requests"},
		Short:   "List local contact invites",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			identity, ok, err := svc.GetIdentity(cmd.Context())
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("identity is not initialized; run `aqua init`")
			}

			effectiveIncomingOnly := true
			effectiveStatus := "pending"
			if all {
				effectiveIncomingOnly = false
				effectiveStatus = ""
			}
			if cmd.Flags().Changed("incoming") {
				effectiveIncomingOnly = incomingOnly
			}
			if cmd.Flags().Changed("status") {
				effectiveStatus = status
			}

			requests, err := svc.ListContactRequests(cmd.Context())
			if err != nil {
				return err
			}

			parsedStatus := aqua.ContactRequestStatus("")
			if strings.TrimSpace(effectiveStatus) != "" {
				parsedStatus, err = aqua.ParseContactRequestStatus(effectiveStatus)
				if err != nil {
					return err
				}
			}

			filtered := make([]aqua.ContactRequest, 0, len(requests))
			for _, request := range requests {
				if parsedStatus != "" && request.Status != parsedStatus {
					continue
				}
				if effectiveIncomingOnly && strings.TrimSpace(request.RecipientPeerID) != identity.PeerID {
					continue
				}
				filtered = append(filtered, request)
			}

			views := contactRequestViewsFromRequests(identity.PeerID, filtered)
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), views)
			}
			if len(views) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no invites")
				return nil
			}
			for i, view := range views {
				_, _ = fmt.Fprintf(
					cmd.OutOrStdout(),
					"[%d]\nrequest_id: %s\ndirection: %s\npeer_id: %s\nstatus: %s\nverify_on_accept: %t\ncreated_at: %s\n",
					i+1,
					view.RequestID,
					view.Direction,
					view.PeerID,
					view.Status,
					view.VerifyOnAccept,
					view.CreatedAt.UTC().Format(time.RFC3339),
				)
				if strings.TrimSpace(view.DisplayName) != "" {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "display_name: %s\n", view.DisplayName)
				}
				if view.RespondedAt != nil {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "responded_at: %s\n", view.RespondedAt.UTC().Format(time.RFC3339))
				}
				if i < len(views)-1 {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "")
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "Filter by request status: pending|accepted|rejected")
	cmd.Flags().BoolVar(&incomingOnly, "incoming", false, "Only show incoming requests")
	cmd.Flags().BoolVar(&all, "all", false, "Show all invites instead of only pending incoming ones")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newContactsImportCmd() *cobra.Command {
	var displayName string
	var outputJSON bool
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
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"status":      status,
					"peer_id":     result.Contact.PeerID,
					"node_uuid":   result.Contact.NodeUUID,
					"trust_state": result.Contact.TrustState,
					"conflicted":  result.Conflicted,
				})
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "status: %s\npeer_id: %s\nnode_uuid: %s\ntrust_state: %s\n", status, result.Contact.PeerID, result.Contact.NodeUUID, result.Contact.TrustState)
			return nil
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "Display name override for this contact")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newContactsInviteCmd() *cobra.Command {
	var displayName string
	var verify bool
	var outputJSON bool
	var relayMode string
	var localOnly bool
	var addresses []string
	var listenAddrs []string
	var relayAddrs []string
	var advertiseMode string

	cmd := &cobra.Command{
		Use:     "invite <address>",
		Aliases: []string{"add"},
		Short:   "Create and send a contact invite to a peer",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			now := time.Now().UTC()
			dialAddress := strings.TrimSpace(args[0])
			peerID, err := extractPeerIDFromDialAddress(dialAddress)
			if err != nil {
				return err
			}

			svc := serviceFromCmd(cmd)
			localCardRaw, err := exportLocalContactCardForCommand(cmd, svc, addresses, listenAddrs, relayAddrs, advertiseMode, now)
			if err != nil {
				return err
			}

			request, err := svc.CreateContactRequest(cmd.Context(), peerID, localCardRaw, displayName, verify, now)
			if err != nil {
				symbol := aqua.SymbolOf(err)
				if symbol != "" {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "symbol: %s\n", symbol)
				}
				return err
			}

			if !localOnly {
				payloadRaw, err := aqua.EncodeContactRequestControlMessage(request)
				if err != nil {
					return err
				}
				if err := sendContactControlToPeer(cmd, relayMode, request.RecipientPeerID, []string{dialAddress}, payloadRaw); err != nil {
					return fmt.Errorf("contact request %s stored locally but delivery failed: %w", request.RequestID, err)
				}
			}

			view := contactRequestViewFromRequest(request.InitiatorPeerID, request)
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), view)
			}
			_, _ = fmt.Fprintf(
				cmd.OutOrStdout(),
				"request_id: %s\npeer_id: %s\nstatus: %s\nverify_on_accept: %t\n",
				view.RequestID,
				view.PeerID,
				view.Status,
				view.VerifyOnAccept,
			)
			return nil
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "Preferred local alias for the peer if the request is accepted")
	cmd.Flags().BoolVar(&verify, "verify", false, "Mark both contacts as verified if the request is accepted")
	cmd.Flags().StringVar(&relayMode, "relay-mode", "auto", "Relay dial mode: auto|off|required")
	cmd.Flags().BoolVar(&localOnly, "local-only", false, "Only mutate local state and skip network delivery")
	cmd.Flags().StringArrayVar(&addresses, "address", nil, "Local contact multiaddr to include in the request (repeatable)")
	cmd.Flags().StringArrayVar(&listenAddrs, "listen", nil, "Configured local listen multiaddr (repeatable), used when --address is empty")
	cmd.Flags().StringArrayVar(&relayAddrs, "relay", nil, "Relay endpoint multiaddr to include in the local contact card (repeatable)")
	cmd.Flags().StringVar(&advertiseMode, "advertise", "auto", "Local address advertise mode: auto|direct|relay|both")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	cmd.AddCommand(newContactsInviteAcceptCmd())
	cmd.AddCommand(newContactsInviteRejectCmd())
	return cmd
}

func newContactsInviteAcceptCmd() *cobra.Command {
	var displayName string
	var outputJSON bool
	var relayMode string
	var localOnly bool
	var addresses []string
	var listenAddrs []string
	var relayAddrs []string
	var advertiseMode string

	cmd := &cobra.Command{
		Use:   "accept <request_id|peer_id>",
		Short: "Accept an incoming contact request and notify the requester",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			now := time.Now().UTC()
			svc := serviceFromCmd(cmd)
			request, contact, err := svc.AcceptContactRequest(cmd.Context(), args[0], displayName, now)
			if err != nil {
				symbol := aqua.SymbolOf(err)
				if symbol != "" {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "symbol: %s\n", symbol)
				}
				return err
			}

			if !localOnly {
				localCardRaw, err := exportLocalContactCardForCommand(cmd, svc, addresses, listenAddrs, relayAddrs, advertiseMode, now)
				if err != nil {
					return fmt.Errorf("contact request %s accepted locally but delivery failed: %w", request.RequestID, err)
				}
				payloadRaw, err := aqua.EncodeContactDecisionControlMessage(request, aqua.ContactControlActionAccept, localCardRaw, now)
				if err != nil {
					return fmt.Errorf("contact request %s accepted locally but delivery failed: %w", request.RequestID, err)
				}
				peerID, peerAddrs, err := resolveRequesterDialTarget(request)
				if err != nil {
					return fmt.Errorf("contact request %s accepted locally but delivery failed: %w", request.RequestID, err)
				}
				if err := sendContactControlToPeer(cmd, relayMode, peerID, peerAddrs, payloadRaw); err != nil {
					return fmt.Errorf("contact request %s accepted locally but delivery failed: %w", request.RequestID, err)
				}
			}

			view := contactRequestViewFromRequest(request.RecipientPeerID, request)
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"request": view,
					"contact": contactSummaryViewFromContact(contact),
				})
			}
			_, _ = fmt.Fprintf(
				cmd.OutOrStdout(),
				"request_id: %s\npeer_id: %s\nstatus: %s\ntrust_state: %s\n",
				view.RequestID,
				view.PeerID,
				view.Status,
				contact.TrustState,
			)
			return nil
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "Display name override for the accepted contact")
	cmd.Flags().StringVar(&relayMode, "relay-mode", "auto", "Relay dial mode: auto|off|required")
	cmd.Flags().BoolVar(&localOnly, "local-only", false, "Only mutate local state and skip network delivery")
	cmd.Flags().StringArrayVar(&addresses, "address", nil, "Local contact multiaddr to include in the accept response (repeatable)")
	cmd.Flags().StringArrayVar(&listenAddrs, "listen", nil, "Configured local listen multiaddr (repeatable), used when --address is empty")
	cmd.Flags().StringArrayVar(&relayAddrs, "relay", nil, "Relay endpoint multiaddr to include in the local contact card (repeatable)")
	cmd.Flags().StringVar(&advertiseMode, "advertise", "auto", "Local address advertise mode: auto|direct|relay|both")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newContactsInviteRejectCmd() *cobra.Command {
	var outputJSON bool
	var relayMode string
	var localOnly bool

	cmd := &cobra.Command{
		Use:   "reject <request_id|peer_id>",
		Short: "Reject an incoming contact request and notify the requester",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			now := time.Now().UTC()
			svc := serviceFromCmd(cmd)
			request, err := svc.RejectContactRequest(cmd.Context(), args[0], now)
			if err != nil {
				symbol := aqua.SymbolOf(err)
				if symbol != "" {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "symbol: %s\n", symbol)
				}
				return err
			}

			if !localOnly {
				payloadRaw, err := aqua.EncodeContactDecisionControlMessage(request, aqua.ContactControlActionReject, nil, now)
				if err != nil {
					return fmt.Errorf("contact request %s rejected locally but delivery failed: %w", request.RequestID, err)
				}
				peerID, peerAddrs, err := resolveRequesterDialTarget(request)
				if err != nil {
					return fmt.Errorf("contact request %s rejected locally but delivery failed: %w", request.RequestID, err)
				}
				if err := sendContactControlToPeer(cmd, relayMode, peerID, peerAddrs, payloadRaw); err != nil {
					return fmt.Errorf("contact request %s rejected locally but delivery failed: %w", request.RequestID, err)
				}
			}

			view := contactRequestViewFromRequest(request.RecipientPeerID, request)
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), view)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "request_id: %s\npeer_id: %s\nstatus: %s\n", view.RequestID, view.PeerID, view.Status)
			return nil
		},
	}
	cmd.Flags().StringVar(&relayMode, "relay-mode", "auto", "Relay dial mode: auto|off|required")
	cmd.Flags().BoolVar(&localOnly, "local-only", false, "Only mutate local state and skip network delivery")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newContactsDelCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "del <peer_id>",
		Short: "Delete a contact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			peerID := strings.TrimSpace(args[0])
			if err := svc.DeleteContact(cmd.Context(), peerID, time.Now().UTC()); err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"deleted": true,
					"peer_id": peerID,
				})
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "deleted: %s\n", peerID)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
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
	var outputJSON bool
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
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"peer_id":     contact.PeerID,
					"trust_state": contact.TrustState,
				})
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "peer_id: %s\ntrust_state: %s\n", contact.PeerID, contact.TrustState)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

type contactRequestView struct {
	RequestID       string                    `json:"request_id"`
	Direction       string                    `json:"direction"`
	PeerID          string                    `json:"peer_id"`
	InitiatorPeerID string                    `json:"initiator_peer_id"`
	RecipientPeerID string                    `json:"recipient_peer_id"`
	DisplayName     string                    `json:"display_name,omitempty"`
	VerifyOnAccept  bool                      `json:"verify_on_accept"`
	Status          aqua.ContactRequestStatus `json:"status"`
	CreatedAt       time.Time                 `json:"created_at"`
	RespondedAt     *time.Time                `json:"responded_at,omitempty"`
}

func contactRequestViewFromRequest(localPeerID string, request aqua.ContactRequest) contactRequestView {
	direction := "outgoing"
	peerID := strings.TrimSpace(request.RecipientPeerID)
	if strings.TrimSpace(localPeerID) == strings.TrimSpace(request.RecipientPeerID) {
		direction = "incoming"
		peerID = strings.TrimSpace(request.InitiatorPeerID)
	}
	return contactRequestView{
		RequestID:       strings.TrimSpace(request.RequestID),
		Direction:       direction,
		PeerID:          peerID,
		InitiatorPeerID: strings.TrimSpace(request.InitiatorPeerID),
		RecipientPeerID: strings.TrimSpace(request.RecipientPeerID),
		DisplayName:     strings.TrimSpace(request.DisplayName),
		VerifyOnAccept:  request.VerifyOnAccept,
		Status:          request.Status,
		CreatedAt:       request.CreatedAt,
		RespondedAt:     request.RespondedAt,
	}
}

func contactRequestViewsFromRequests(localPeerID string, requests []aqua.ContactRequest) []contactRequestView {
	out := make([]contactRequestView, 0, len(requests))
	for _, request := range requests {
		out = append(out, contactRequestViewFromRequest(localPeerID, request))
	}
	return out
}

func contactSummaryViewFromContact(contact aqua.Contact) map[string]any {
	fingerprint, _ := aqua.FingerprintGrouped(contact.IdentityPubEd25519)
	return map[string]any{
		"peer_id":      contact.PeerID,
		"node_uuid":    contact.NodeUUID,
		"display_name": contact.DisplayName,
		"nickname":     contact.Nickname,
		"trust_state":  contact.TrustState,
		"fingerprint":  fingerprint,
	}
}

func exportLocalContactCardForCommand(cmd *cobra.Command, svc *aqua.Service, addresses []string, listenAddrs []string, relayAddrs []string, advertiseMode string, now time.Time) ([]byte, error) {
	resolvedAddresses, err := resolveContactCardAdvertiseAddressesForCommand(
		cmd.Context(),
		svc,
		addresses,
		listenAddrs,
		relayAddrs,
		advertiseMode,
		cmd.InOrStdin(),
		cmd.ErrOrStderr(),
	)
	if err != nil {
		return nil, err
	}
	if len(resolvedAddresses) == 0 {
		mode := strings.ToLower(strings.TrimSpace(advertiseMode))
		if mode == "" {
			mode = "auto"
		}
		return nil, fmt.Errorf("no addresses available for advertise mode %q", mode)
	}
	_, rawCard, err := svc.ExportContactCard(cmd.Context(), resolvedAddresses, aqua.ProtocolVersionV1, aqua.ProtocolVersionV1, now, nil)
	if err != nil {
		return nil, err
	}
	return rawCard, nil
}

func sendContactControlToPeer(cmd *cobra.Command, relayMode string, peerID string, addresses []string, payloadRaw []byte) error {
	node, err := newDialNodeWithRelayMode(cmd, relayMode)
	if err != nil {
		return err
	}
	defer node.Close()

	return node.PushContactControl(cmd.Context(), strings.TrimSpace(peerID), addresses, payloadRaw)
}

func resolveRequesterDialTarget(request aqua.ContactRequest) (string, []string, error) {
	cardRaw := []byte(strings.TrimSpace(request.InitiatorCardJSON))
	cardNow := request.CreatedAt
	if cardNow.IsZero() {
		cardNow = time.Now().UTC()
	}
	parsed, err := aqua.ParseAndVerifyContactCard(cardRaw, cardNow)
	if err != nil {
		return "", nil, err
	}
	peerID := strings.TrimSpace(parsed.Card.Payload.PeerID)
	if peerID != strings.TrimSpace(request.InitiatorPeerID) {
		return "", nil, fmt.Errorf("stored request card peer_id mismatch")
	}
	return peerID, append([]string(nil), parsed.Card.Payload.Addresses...), nil
}
