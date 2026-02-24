package aqua

import (
	"fmt"
	"strings"
	"time"
)

const (
	GroupControlTopicV1 = "group.control.v1"
	GroupMessageTopicV1 = "group.message.v1"

	GroupInviteTTL       = 7 * 24 * time.Hour
	GroupInviteClockSkew = 5 * time.Minute
	GroupMaxMembers      = 256
	GroupReplayWindow    = 64
)

type GroupRole string

const (
	GroupRoleManager GroupRole = "manager"
	GroupRoleMember  GroupRole = "member"
)

type GroupInviteStatus string

const (
	GroupInviteStatusPending  GroupInviteStatus = "pending"
	GroupInviteStatusAccepted GroupInviteStatus = "accepted"
	GroupInviteStatusRejected GroupInviteStatus = "rejected"
	GroupInviteStatusExpired  GroupInviteStatus = "expired"
)

type Group struct {
	GroupID        string    `json:"group_id"`
	Epoch          int       `json:"epoch"`
	MaxMembers     int       `json:"max_members"`
	ManagerPeerIDs []string  `json:"manager_peer_ids"`
	LocalRole      GroupRole `json:"local_role,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type GroupRoleEntry struct {
	PeerID    string    `json:"peer_id"`
	Role      GroupRole `json:"role"`
	UpdatedAt time.Time `json:"updated_at"`
}

type GroupRoleState struct {
	GroupID     string           `json:"group_id"`
	RoleVersion int              `json:"role_version"`
	Roles       []GroupRoleEntry `json:"roles"`
}

type GroupMember struct {
	GroupID    string     `json:"group_id"`
	PeerID     string     `json:"peer_id"`
	JoinedAt   time.Time  `json:"joined_at"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type GroupInvite struct {
	GroupID       string            `json:"group_id"`
	InviteID      string            `json:"invite_id"`
	InviterPeerID string            `json:"inviter_peer_id"`
	InviteePeerID string            `json:"invitee_peer_id"`
	CreatedAt     time.Time         `json:"created_at"`
	ExpiresAt     time.Time         `json:"expires_at"`
	Status        GroupInviteStatus `json:"status"`
	RespondedAt   *time.Time        `json:"responded_at,omitempty"`
}

type GroupDetails struct {
	Group     Group          `json:"group"`
	RoleState GroupRoleState `json:"role_state"`
	Members   []GroupMember  `json:"members"`
	Invites   []GroupInvite  `json:"invites"`
}

func ParseGroupRole(raw string) (GroupRole, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(GroupRoleManager):
		return GroupRoleManager, nil
	case string(GroupRoleMember):
		return GroupRoleMember, nil
	default:
		return "", fmt.Errorf("invalid group role %q (supported: %s, %s)", raw, GroupRoleManager, GroupRoleMember)
	}
}

func ParseGroupInviteStatus(raw string) (GroupInviteStatus, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(GroupInviteStatusPending):
		return GroupInviteStatusPending, nil
	case string(GroupInviteStatusAccepted):
		return GroupInviteStatusAccepted, nil
	case string(GroupInviteStatusRejected):
		return GroupInviteStatusRejected, nil
	case string(GroupInviteStatusExpired):
		return GroupInviteStatusExpired, nil
	default:
		return "", fmt.Errorf("invalid group invite status %q", raw)
	}
}
