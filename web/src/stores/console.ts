import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { APIError, apiRequest, apiURL, apiWebSocketURL } from '../api'
import type {
  Contact,
  Conversation,
  Group,
  GroupInvite,
  InboxMessage,
  OutboxMessage,
  RuntimeView,
  ViewName
} from '../types'
import {
  buildConversations,
  buildTimeline,
  labelForPeerFromMap,
  relaySummary
} from '../utils/messages'

export const useConsoleStore = defineStore('console', () => {
  const activeView = ref<ViewName>('messages')
  const loading = ref(false)
  const errorText = ref('')
  const runtime = ref<RuntimeView | null>(null)
  const contacts = ref<Contact[]>([])
  const inbox = ref<InboxMessage[]>([])
  const outbox = ref<OutboxMessage[]>([])
  const groups = ref<Group[]>([])
  const invites = ref<GroupInvite[]>([])
  const selectedConversationId = ref('')
  const pendingDirectPeerID = ref('')
  const wsState = ref<'connecting' | 'open' | 'closed'>('closed')

  const contactMap = computed(() => {
    const map = new Map<string, Contact>()
    for (const contact of contacts.value) {
      map.set(contact.peer_id, contact)
    }
    return map
  })

  const conversations = computed<Conversation[]>(() => {
    const items = buildConversations({
      inbox: inbox.value,
      outbox: outbox.value,
      groups: groups.value,
      labelForPeer
    })
    if (pendingDirectPeerID.value) {
      const id = `direct:${pendingDirectPeerID.value}`
      if (!items.some((item) => item.id === id)) {
        items.unshift({
          id,
          kind: 'direct',
          title: labelForPeer(pendingDirectPeerID.value),
          updated_at: new Date(0).toISOString(),
          unread_count: 0,
          last_preview: ''
        })
      }
    }
    return items
  })

  watch(
    conversations,
    (items) => {
      if (!items.length) {
        selectedConversationId.value = ''
        return
      }
      if (!items.some((item) => item.id === selectedConversationId.value)) {
        selectedConversationId.value = items[0].id
      }
    },
    { immediate: true }
  )

  const selectedConversation = computed(
    () => conversations.value.find((item) => item.id === selectedConversationId.value) ?? null
  )

  const selectedTimeline = computed(() =>
    buildTimeline({
      conversation: selectedConversation.value,
      inbox: inbox.value,
      outbox: outbox.value
    })
  )

  const pendingInvites = computed(() => invites.value.filter((invite) => invite.status === 'pending'))
  const selectedGroup = computed(() => {
    const selected = selectedConversation.value
    if (!selected || selected.kind !== 'group') return null
    return groups.value.find((item) => item.group_id === selected.id.replace('group:', '')) ?? null
  })

  let ws: WebSocket | null = null
  let refreshTimer: number | null = null
  let reconnectTimer: number | null = null
  let eventsActive = false

  function labelForPeer(peerID: string): string {
    return labelForPeerFromMap(peerID, contactMap.value)
  }

  function setActiveView(view: ViewName): void {
    activeView.value = view
  }

  function selectConversation(id: string): void {
    selectedConversationId.value = id
    pendingDirectPeerID.value = id.startsWith('direct:') ? id.replace('direct:', '') : ''
  }

  function openDirectConversation(peerID: string): void {
    pendingDirectPeerID.value = peerID
    activeView.value = 'messages'
    selectedConversationId.value = `direct:${peerID}`
  }

  function clearError(): void {
    errorText.value = ''
  }

  function setError(error: unknown): void {
    errorText.value = formatError(error)
  }

  async function refreshAll(): Promise<void> {
    loading.value = true
    errorText.value = ''
    try {
      const [runtimeValue, contactsValue, inboxValue, outboxValue, groupsValue, invitesValue] = await Promise.all([
        apiRequest<RuntimeView>(apiURL('/api/console/v1/node/runtime')),
        apiRequest<Contact[]>(apiURL('/api/console/v1/contacts')),
        apiRequest<InboxMessage[]>(apiURL('/api/console/v1/inbox?limit=200')),
        apiRequest<OutboxMessage[]>(apiURL('/api/console/v1/outbox?limit=200')),
        apiRequest<Group[]>(apiURL('/api/console/v1/groups')),
        apiRequest<GroupInvite[]>(apiURL('/api/console/v1/groups/invites?incoming=true'))
      ])
      runtime.value = runtimeValue
      contacts.value = contactsValue
      inbox.value = inboxValue
      outbox.value = outboxValue
      groups.value = groupsValue
      invites.value = invitesValue
    } catch (error) {
      setError(error)
    } finally {
      loading.value = false
    }
  }

  function scheduleRefresh(): void {
    if (refreshTimer !== null) window.clearTimeout(refreshTimer)
    refreshTimer = window.setTimeout(() => {
      void refreshAll()
      refreshTimer = null
    }, 150)
  }

  function clearReconnectTimer(): void {
    if (reconnectTimer !== null) {
      window.clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
  }

  function disconnectEvents(): void {
    clearReconnectTimer()
    if (ws) {
      ws.onopen = null
      ws.onmessage = null
      ws.onclose = null
      ws.close()
      ws = null
    }
    wsState.value = 'closed'
  }

  function connectEvents(): void {
    disconnectEvents()
    wsState.value = 'connecting'
    ws = new WebSocket(apiWebSocketURL('/api/console/v1/ws/events'))
    ws.onopen = () => {
      wsState.value = 'open'
    }
    ws.onmessage = () => {
      scheduleRefresh()
    }
    ws.onclose = () => {
      wsState.value = 'closed'
      ws = null
      if (!eventsActive) return
      clearReconnectTimer()
      reconnectTimer = window.setTimeout(() => {
        reconnectTimer = null
        connectEvents()
      }, 1500)
    }
  }

  function start(): void {
    if (eventsActive) return
    eventsActive = true
    void refreshAll()
    connectEvents()
  }

  function stop(): void {
    eventsActive = false
    if (refreshTimer !== null) {
      window.clearTimeout(refreshTimer)
      refreshTimer = null
    }
    disconnectEvents()
  }

  async function sendDirectMessage(peerID: string, message: string): Promise<boolean> {
    try {
      await apiRequest(apiURL('/api/console/v1/send/direct'), {
        method: 'POST',
        body: JSON.stringify({
          peer_id: peerID,
          message
        })
      })
      await refreshAll()
      return true
    } catch (error) {
      setError(error)
      return false
    }
  }

  async function sendGroupMessage(groupID: string, message: string): Promise<boolean> {
    try {
      await apiRequest(apiURL(`/api/console/v1/groups/${groupID}/messages`), {
        method: 'POST',
        body: JSON.stringify({
          message
        })
      })
      await refreshAll()
      return true
    } catch (error) {
      setError(error)
      return false
    }
  }

  async function addContactFromAddress(address: string, displayName: string, verify: boolean): Promise<boolean> {
    try {
      await apiRequest(apiURL('/api/console/v1/contacts/fetch'), {
        method: 'POST',
        body: JSON.stringify({
          address,
          display_name: displayName,
          verify
        })
      })
      await refreshAll()
      return true
    } catch (error) {
      setError(error)
      return false
    }
  }

  async function createGroup(): Promise<Group | null> {
    try {
      const created = await apiRequest<Group>(apiURL('/api/console/v1/groups'), {
        method: 'POST',
        body: JSON.stringify({})
      })
      await refreshAll()
      return created
    } catch (error) {
      setError(error)
      return null
    }
  }

  async function inviteToGroup(groupID: string, peerID: string): Promise<boolean> {
    try {
      await apiRequest(apiURL(`/api/console/v1/groups/${groupID}/invites`), {
        method: 'POST',
        body: JSON.stringify({
          peer_id: peerID
        })
      })
      await refreshAll()
      return true
    } catch (error) {
      setError(error)
      return false
    }
  }

  async function resolveInvite(groupID: string, inviteID: string, action: 'accept' | 'reject'): Promise<boolean> {
    try {
      await apiRequest(apiURL(`/api/console/v1/groups/${groupID}/invites/${inviteID}/${action}`), {
        method: 'POST',
        body: JSON.stringify({})
      })
      await refreshAll()
      return true
    } catch (error) {
      setError(error)
      return false
    }
  }

  async function markMessageRead(messageID: string): Promise<boolean> {
    try {
      await apiRequest(apiURL('/api/console/v1/inbox/mark-read'), {
        method: 'POST',
        body: JSON.stringify({
          message_ids: [messageID]
        })
      })
      await refreshAll()
      return true
    } catch (error) {
      setError(error)
      return false
    }
  }

  return {
    activeView,
    loading,
    errorText,
    runtime,
    contacts,
    inbox,
    outbox,
    groups,
    invites,
    selectedConversationId,
    wsState,
    contactMap,
    conversations,
    selectedConversation,
    selectedTimeline,
    pendingInvites,
    selectedGroup,
    labelForPeer,
    relaySummary,
    setActiveView,
    selectConversation,
    openDirectConversation,
    clearError,
    refreshAll,
    start,
    stop,
    sendDirectMessage,
    sendGroupMessage,
    addContactFromAddress,
    createGroup,
    inviteToGroup,
    resolveInvite,
    markMessageRead
  }
})

function formatError(error: unknown): string {
  if (error instanceof APIError) {
    return `${error.code}: ${error.message}${error.suggestion ? ` | ${error.suggestion}` : ''}`
  }
  return error instanceof Error ? error.message : String(error)
}
