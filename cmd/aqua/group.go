package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/aqua/aqua"
	"github.com/spf13/cobra"
)

func newGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "group",
		Short: "Manage local group control-plane state",
	}
	cmd.AddCommand(newGroupCreateCmd())
	cmd.AddCommand(newGroupInviteCmd())
	cmd.AddCommand(newGroupRemoveMemberCmd())
	cmd.AddCommand(newGroupRoleCmd())
	cmd.AddCommand(newGroupListCmd())
	cmd.AddCommand(newGroupShowCmd())
	cmd.AddCommand(newGroupSendCmd())
	return cmd
}

func newGroupCreateCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a group with local peer as initial manager",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			group, err := svc.CreateGroup(cmd.Context(), time.Now().UTC())
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), group)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "group_id: %s\nepoch: %d\nmax_members: %d\nlocal_role: %s\n", group.GroupID, group.Epoch, group.MaxMembers, group.LocalRole)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newGroupInviteCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "invite <group_id> <peer_id>",
		Short: "Create an invite for a peer",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			invite, err := svc.InviteGroupMember(cmd.Context(), args[0], args[1], time.Now().UTC())
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), invite)
			}
			_, _ = fmt.Fprintf(
				cmd.OutOrStdout(),
				"group_id: %s\ninvite_id: %s\ninvitee_peer_id: %s\nstatus: %s\ncreated_at: %s\nexpires_at: %s\n",
				invite.GroupID,
				invite.InviteID,
				invite.InviteePeerID,
				invite.Status,
				invite.CreatedAt.UTC().Format(time.RFC3339),
				invite.ExpiresAt.UTC().Format(time.RFC3339),
			)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	cmd.AddCommand(newGroupInviteAcceptCmd())
	cmd.AddCommand(newGroupInviteRejectCmd())
	return cmd
}

func newGroupInviteAcceptCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "accept <group_id> <invite_id>",
		Short: "Accept an invite and activate membership locally",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			invite, group, err := svc.AcceptGroupInvite(cmd.Context(), args[0], args[1], time.Now().UTC())
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"invite": invite,
					"group":  group,
				})
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "group_id: %s\ninvite_id: %s\nstatus: %s\nepoch: %d\n", invite.GroupID, invite.InviteID, invite.Status, group.Epoch)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newGroupInviteRejectCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "reject <group_id> <invite_id>",
		Short: "Reject an invite",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			invite, err := svc.RejectGroupInvite(cmd.Context(), args[0], args[1], time.Now().UTC())
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), invite)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "group_id: %s\ninvite_id: %s\nstatus: %s\n", invite.GroupID, invite.InviteID, invite.Status)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newGroupRemoveMemberCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "remove-member <group_id> <peer_id>",
		Short: "Remove a member from a group",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			group, err := svc.RemoveGroupMember(cmd.Context(), args[0], args[1], time.Now().UTC())
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), group)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "group_id: %s\nepoch: %d\nremoved_peer_id: %s\n", group.GroupID, group.Epoch, strings.TrimSpace(args[1]))
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newGroupRoleCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "role <group_id> <peer_id> <manager|member>",
		Short: "Update group role for a member",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			role, err := aqua.ParseGroupRole(args[2])
			if err != nil {
				return err
			}
			svc := serviceFromCmd(cmd)
			roleState, group, err := svc.SetGroupRole(cmd.Context(), args[0], args[1], role, time.Now().UTC())
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"group":      group,
					"role_state": roleState,
				})
			}
			_, _ = fmt.Fprintf(
				cmd.OutOrStdout(),
				"group_id: %s\npeer_id: %s\nrole: %s\nrole_version: %d\n",
				group.GroupID,
				strings.TrimSpace(args[1]),
				role,
				roleState.RoleVersion,
			)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newGroupListCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List local groups",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			groups, err := svc.ListGroups(cmd.Context())
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), groups)
			}
			if len(groups) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no groups")
				return nil
			}
			for i, group := range groups {
				_, _ = fmt.Fprintf(
					cmd.OutOrStdout(),
					"[%d]\ngroup_id: %s\nepoch: %d\nmax_members: %d\nlocal_role: %s\nupdated_at: %s\n",
					i+1,
					group.GroupID,
					group.Epoch,
					group.MaxMembers,
					group.LocalRole,
					group.UpdatedAt.UTC().Format(time.RFC3339),
				)
				if i < len(groups)-1 {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "")
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newGroupShowCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "show <group_id>",
		Short: "Show group details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			details, err := svc.GetGroupDetails(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), details)
			}

			pendingInvites := 0
			for _, invite := range details.Invites {
				if invite.Status == aqua.GroupInviteStatusPending {
					pendingInvites++
				}
			}
			_, _ = fmt.Fprintf(
				cmd.OutOrStdout(),
				"group_id: %s\nepoch: %d\nmax_members: %d\nmy_role: %s\nrole_version: %d\nactive_members: %d\npending_invites: %d\nmanagers: %s\n",
				details.Group.GroupID,
				details.Group.Epoch,
				details.Group.MaxMembers,
				details.Group.LocalRole,
				details.RoleState.RoleVersion,
				len(details.Members),
				pendingInvites,
				strings.Join(details.Group.ManagerPeerIDs, ","),
			)
			if len(details.Members) > 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "members:")
				for _, member := range details.Members {
					lastSeen := ""
					if member.LastSeenAt != nil {
						lastSeen = member.LastSeenAt.UTC().Format(time.RFC3339)
					}
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  - peer_id: %s role: %s last_seen_at: %s\n", member.PeerID, roleForMember(details.RoleState, member.PeerID), lastSeen)
				}
			}
			if len(details.Invites) > 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "invites:")
				for _, invite := range details.Invites {
					_, _ = fmt.Fprintf(
						cmd.OutOrStdout(),
						"  - invite_id: %s invitee_peer_id: %s status: %s expires_at: %s\n",
						invite.InviteID,
						invite.InviteePeerID,
						invite.Status,
						invite.ExpiresAt.UTC().Format(time.RFC3339),
					)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newGroupSendCmd() *cobra.Command {
	var relayMode string
	var contentType string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "send <group_id> <message>",
		Short: "Send a group message to currently known members",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			groupID := strings.TrimSpace(args[0])
			message := args[1]
			if groupID == "" {
				return fmt.Errorf("group_id is required")
			}
			svc := serviceFromCmd(cmd)
			details, err := svc.GetGroupDetails(cmd.Context(), groupID)
			if err != nil {
				return err
			}
			if details.Group.LocalRole == "" {
				return fmt.Errorf("local peer is not an active member of group %s", groupID)
			}
			localIdentity, ok, err := svc.GetIdentity(cmd.Context())
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("identity is not initialized; run `aqua init`")
			}

			node, err := newDialNodeWithRelayMode(cmd, relayMode)
			if err != nil {
				return err
			}
			defer node.Close()

			envelope := map[string]any{
				"version":        1,
				"group_id":       groupID,
				"epoch":          details.Group.Epoch,
				"sender_peer_id": localIdentity.PeerID,
				"content_type":   contentType,
				"message":        message,
			}
			payloadRaw, err := encodeJSON(envelope)
			if err != nil {
				return err
			}
			payloadBase64 := base64.RawURLEncoding.EncodeToString(payloadRaw)
			idempotencyKey := messageEnvelopeKey("group:" + groupID + ":" + uuid.NewString())

			type sendFailure struct {
				PeerID string `json:"peer_id"`
				Error  string `json:"error"`
			}
			failures := make([]sendFailure, 0)
			attempted := 0
			sent := 0
			for _, member := range details.Members {
				peerID := strings.TrimSpace(member.PeerID)
				if peerID == "" || peerID == localIdentity.PeerID {
					continue
				}
				attempted++
				_, err := node.PushData(cmd.Context(), peerID, nil, aqua.DataPushRequest{
					Topic:          aqua.GroupMessageTopicV1,
					ContentType:    "application/json",
					PayloadBase64:  payloadBase64,
					IdempotencyKey: idempotencyKey + ":" + idempotencyToken(peerID),
				}, false)
				if err != nil {
					failures = append(failures, sendFailure{PeerID: peerID, Error: err.Error()})
					continue
				}
				sent++
			}

			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"group_id":         groupID,
					"epoch":            details.Group.Epoch,
					"topic":            aqua.GroupMessageTopicV1,
					"attempted_peers":  attempted,
					"sent_peers":       sent,
					"failed_peers":     len(failures),
					"failures":         failures,
					"local_sender_id":  localIdentity.PeerID,
					"message_preview":  message,
					"payload_encoding": "json+base64url",
				})
			}
			_, _ = fmt.Fprintf(
				cmd.OutOrStdout(),
				"group_id: %s\nepoch: %d\ntopic: %s\nattempted_peers: %d\nsent_peers: %d\nfailed_peers: %d\n",
				groupID,
				details.Group.Epoch,
				aqua.GroupMessageTopicV1,
				attempted,
				sent,
				len(failures),
			)
			for _, failure := range failures {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "failure: peer_id=%s err=%s\n", failure.PeerID, failure.Error)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&relayMode, "relay-mode", "auto", "Relay dial mode: auto|off|required")
	cmd.Flags().StringVar(&contentType, "content-type", "text/plain", "Group message content type")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func roleForMember(state aqua.GroupRoleState, peerID string) aqua.GroupRole {
	peerID = strings.TrimSpace(peerID)
	for _, role := range state.Roles {
		if strings.TrimSpace(role.PeerID) == peerID {
			return role.Role
		}
	}
	return aqua.GroupRoleMember
}

func encodeJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal json payload: %w", err)
	}
	return raw, nil
}
