export interface APIErrorBody {
  code: string
  message: string
  details?: string
  suggestion?: string
}

export interface APIErrorEnvelope {
  error: APIErrorBody
}

export type ViewName = 'messages' | 'contacts' | 'node' | 'settings'

export interface IdentityView {
  node_uuid: string
  peer_id: string
  node_id: string
  nickname?: string
  identity_pub_ed25519?: string
  fingerprint?: string
  created_at?: string
  updated_at?: string
}

export interface RuntimeView {
  status: string
  started_at: string
  version: string
  commit: string
  date: string
  node: IdentityView
  listen_addrs: string[]
  addresses: string[]
  relay_addrs: string[]
  relay_mode: string
  relay_probe: boolean
  web_console_addr: string
  console_web_dst_dir: string
  unread_count: number
  contacts_count: number
  groups_count: number
  recent_relay_events: RelayEvent[]
}

export interface RelayEvent {
  event: string
  path?: string
  relay_peer_id?: string
  target_peer_id?: string
  reason?: string
  timestamp: string
}

export interface Contact {
  node_uuid: string
  peer_id: string
  node_id: string
  display_name?: string
  nickname?: string
  identity_pub_ed25519?: string
  addresses: string[]
  trust_state: 'tofu' | 'verified' | 'conflicted' | 'revoked'
  min_supported_protocol?: number
  max_supported_protocol?: number
  issued_at?: string
  expires_at?: string | null
  updated_at: string
}

export interface InboxMessage {
  message_id: string
  from_peer_id: string
  topic: string
  content_type: string
  payload_base64: string
  payload_text?: string
  idempotency_key: string
  session_id?: string
  reply_to?: string
  read: boolean
  read_at?: string | null
  received_at: string
}

export interface OutboxMessage {
  message_id: string
  to_peer_id: string
  topic: string
  content_type: string
  payload_base64: string
  payload_text?: string
  idempotency_key: string
  session_id?: string
  reply_to?: string
  sent_at: string
}

export interface Group {
  group_id: string
  max_members: number
  local_role?: 'manager' | 'member'
  manager_peer_ids?: string[]
  created_at?: string
  updated_at?: string
}

export interface GroupInvite {
  group_id: string
  invite_id: string
  inviter_peer_id: string
  invitee_peer_id: string
  status: 'pending' | 'accepted' | 'rejected' | 'expired'
  created_at: string
  expires_at: string
  responded_at?: string | null
}

export interface GroupDetails {
  group: Group
  members: Array<{
    peer_id: string
    role: 'manager' | 'member'
    last_seen_at?: string | null
  }>
  invites: GroupInvite[]
}

export interface Conversation {
  id: string
  kind: 'direct' | 'group'
  title: string
  updated_at: string
  unread_count: number
  last_preview: string
}

export interface TimelineMessage {
  id: string
  kind: 'direct' | 'group'
  direction: 'inbound' | 'outbound'
  peer_id?: string
  group_id?: string
  text: string
  topic: string
  content_type: string
  timestamp: string
  read?: boolean
  reply_to?: string
}

export interface GroupEnvelope {
  group_id: string
  sender_peer_id: string
  content_type: string
  message: string
}
