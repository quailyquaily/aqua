import type {
  Contact,
  Conversation,
  Group,
  GroupEnvelope,
  InboxMessage,
  OutboxMessage,
  RelayEvent,
  TimelineMessage
} from '../types'

function truncatePeerID(peerID: string): string {
  return peerID.length > 16 ? `${peerID.slice(0, 8)}...${peerID.slice(-4)}` : peerID
}

export function initialForLabel(label: string): string {
  const trimmed = label.trim()
  if (!trimmed) return '?'
  return trimmed[0]?.toUpperCase() ?? '?'
}

export function labelForPeerFromMap(peerID: string, contactMap: Map<string, Contact>): string {
  const contact = contactMap.get(peerID)
  if (contact?.display_name) return contact.display_name
  if (contact?.nickname) return contact.nickname
  return truncatePeerID(peerID)
}

export function decodeBase64URL(input: string): string {
  const padded = input.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(input.length / 4) * 4, '=')
  const binary = atob(padded)
  const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0))
  return new TextDecoder().decode(bytes)
}

export function parseGroupEnvelope(payloadBase64: string): GroupEnvelope | null {
  try {
    const text = decodeBase64URL(payloadBase64)
    const value = JSON.parse(text) as Record<string, unknown>
    if (typeof value.group_id !== 'string' || typeof value.sender_peer_id !== 'string' || typeof value.message !== 'string') {
      return null
    }
    return {
      group_id: value.group_id,
      sender_peer_id: value.sender_peer_id,
      content_type: typeof value.content_type === 'string' ? value.content_type : 'text/plain',
      message: value.message
    }
  } catch {
    return null
  }
}

export function previewText(message: InboxMessage | OutboxMessage): string {
  if ('payload_text' in message && message.payload_text) {
    return message.payload_text
  }
  if (message.topic === 'group.message.v1') {
    return parseGroupEnvelope(message.payload_base64)?.message ?? '<invalid group payload>'
  }
  try {
    return decodeBase64URL(message.payload_base64)
  } catch {
    return '<binary>'
  }
}

export function buildConversations(input: {
  inbox: InboxMessage[]
  outbox: OutboxMessage[]
  groups: Group[]
  labelForPeer: (peerID: string) => string
}): Conversation[] {
  const result = new Map<string, Conversation>()

  for (const message of input.inbox) {
    if (message.topic === 'group.control.v1') continue
    if (message.topic === 'group.message.v1') {
      const envelope = parseGroupEnvelope(message.payload_base64)
      if (!envelope) continue
      const group = input.groups.find((item) => item.group_id === envelope.group_id)
      const current = result.get(`group:${envelope.group_id}`)
      const updatedAt = message.received_at
      const unread = message.read ? 0 : 1
      const next: Conversation = {
        id: `group:${envelope.group_id}`,
        kind: 'group',
        title: group?.group_id ?? envelope.group_id,
        updated_at: updatedAt,
        unread_count: (current?.unread_count ?? 0) + unread,
        last_preview: envelope.message
      }
      if (!current || current.updated_at < updatedAt) {
        result.set(next.id, next)
      } else {
        result.set(next.id, { ...current, unread_count: next.unread_count })
      }
      continue
    }

    const id = `direct:${message.from_peer_id}`
    const current = result.get(id)
    const next: Conversation = {
      id,
      kind: 'direct',
      title: input.labelForPeer(message.from_peer_id),
      updated_at: message.received_at,
      unread_count: (current?.unread_count ?? 0) + (message.read ? 0 : 1),
      last_preview: previewText(message)
    }
    if (!current || current.updated_at < next.updated_at) {
      result.set(id, next)
    } else {
      result.set(id, { ...current, unread_count: next.unread_count })
    }
  }

  for (const message of input.outbox) {
    if (message.topic === 'group.message.v1') {
      const envelope = parseGroupEnvelope(message.payload_base64)
      if (!envelope) continue
      const id = `group:${envelope.group_id}`
      const current = result.get(id)
      const next: Conversation = {
        id,
        kind: 'group',
        title: envelope.group_id,
        updated_at: message.sent_at,
        unread_count: current?.unread_count ?? 0,
        last_preview: envelope.message
      }
      if (!current || current.updated_at < next.updated_at) {
        result.set(id, next)
      }
      continue
    }

    const id = `direct:${message.to_peer_id}`
    const current = result.get(id)
    const next: Conversation = {
      id,
      kind: 'direct',
      title: input.labelForPeer(message.to_peer_id),
      updated_at: message.sent_at,
      unread_count: current?.unread_count ?? 0,
      last_preview: previewText(message)
    }
    if (!current || current.updated_at < next.updated_at) {
      result.set(id, next)
    }
  }

  for (const group of input.groups) {
    const id = `group:${group.group_id}`
    if (!result.has(id)) {
      result.set(id, {
        id,
        kind: 'group',
        title: group.group_id,
        updated_at: group.updated_at ?? group.created_at ?? new Date(0).toISOString(),
        unread_count: 0,
        last_preview: ''
      })
    }
  }

  return Array.from(result.values()).sort((a, b) => (a.updated_at < b.updated_at ? 1 : -1))
}

export function buildTimeline(input: {
  conversation: Conversation | null
  inbox: InboxMessage[]
  outbox: OutboxMessage[]
}): TimelineMessage[] {
  const selected = input.conversation
  if (!selected) return []

  if (selected.kind === 'direct') {
    const peerID = selected.id.replace('direct:', '')
    const inbound: TimelineMessage[] = input.inbox
      .filter((item) => item.from_peer_id === peerID && item.topic !== 'group.control.v1' && item.topic !== 'group.message.v1')
      .map((item) => ({
        id: item.message_id,
        kind: 'direct',
        direction: 'inbound',
        peer_id: item.from_peer_id,
        text: previewText(item),
        topic: item.topic,
        content_type: item.content_type,
        timestamp: item.received_at,
        read: item.read,
        reply_to: item.reply_to
      }))
    const outbound: TimelineMessage[] = input.outbox
      .filter((item) => item.to_peer_id === peerID && item.topic !== 'group.message.v1')
      .map((item) => ({
        id: item.message_id,
        kind: 'direct',
        direction: 'outbound',
        peer_id: item.to_peer_id,
        text: previewText(item),
        topic: item.topic,
        content_type: item.content_type,
        timestamp: item.sent_at,
        reply_to: item.reply_to
      }))
    return [...inbound, ...outbound].sort((a, b) => (a.timestamp > b.timestamp ? 1 : -1))
  }

  const groupID = selected.id.replace('group:', '')
  const inbound: TimelineMessage[] = input.inbox
    .filter((item) => item.topic === 'group.message.v1')
    .map((item) => ({ raw: item, envelope: parseGroupEnvelope(item.payload_base64) }))
    .filter((item) => item.envelope?.group_id === groupID)
    .map((item) => ({
      id: item.raw.message_id,
      kind: 'group',
      direction: 'inbound',
      group_id: groupID,
      peer_id: item.envelope?.sender_peer_id,
      text: item.envelope?.message ?? '<invalid group payload>',
      topic: item.raw.topic,
      content_type: item.envelope?.content_type ?? item.raw.content_type,
      timestamp: item.raw.received_at,
      read: item.raw.read
    }))

  const outboundMap = new Map<string, TimelineMessage>()
  for (const item of input.outbox.filter((record) => record.topic === 'group.message.v1')) {
    const envelope = parseGroupEnvelope(item.payload_base64)
    if (!envelope || envelope.group_id !== groupID) continue
    const baseID = item.idempotency_key.split(':').slice(0, -1).join(':') || item.idempotency_key
    if (!outboundMap.has(baseID)) {
      outboundMap.set(baseID, {
        id: baseID,
        kind: 'group',
        direction: 'outbound',
        group_id: groupID,
        text: envelope.message,
        topic: item.topic,
        content_type: envelope.content_type,
        timestamp: item.sent_at
      })
    }
  }

  return [...inbound, ...Array.from(outboundMap.values())].sort((a, b) => (a.timestamp > b.timestamp ? 1 : -1))
}

export function formatTimestamp(value: string): string {
  return new Date(value).toLocaleString()
}

export function relaySummary(events: RelayEvent[]): string {
  if (!events.length) return 'No relay events yet'
  const latest = events[events.length - 1]
  return `${latest.event}${latest.relay_peer_id ? ` @ ${latest.relay_peer_id}` : ''}`
}
