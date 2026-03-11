<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useConsoleStore } from '../stores/console'
import { formatTimestamp } from '../utils/messages'

const store = useConsoleStore()
const {
  loading,
  pendingInvites,
  conversations,
  selectedConversationId,
  selectedConversation,
  selectedTimeline,
  selectedGroup
} = storeToRefs(store)

const directDraft = ref('')
const groupDraft = ref('')
const groupInvitePeerID = ref('')
const showInviteDialog = ref(false)
const timelineEl = ref<HTMLElement | null>(null)
const timelineHeaderEl = ref<HTMLElement | null>(null)
const composerEl = ref<HTMLElement | null>(null)
const timelineHeaderHeight = ref(0)
const composerHeight = ref(0)

const orderedConversations = computed(() =>
  [...conversations.value].sort((a, b) => (a.updated_at < b.updated_at ? 1 : -1))
)

const timelineHeaderGroupTitle = computed(() => {
  const selected = selectedConversation.value
  if (!selected || selected.kind !== 'group') return null
  const raw = selected.title
  const prefix = raw.slice(0, 6)
  const suffix = raw.length > 10 ? raw.slice(-4) : ''
  const middle = raw.length > 10 ? raw.slice(6, -4) : ''
  const source = middle ? sampleMaskSource(middle, 14) : []
  return {
    raw,
    prefix: prefix || raw,
    suffix,
    chips: source.map((char, index) => ({
      key: `${char}-${index}`,
      style: mosaicChipStyle(char, index)
    }))
  }
})

const directPeerID = computed(() =>
  selectedConversation.value?.kind === 'direct' ? selectedConversation.value.id.replace('direct:', '') : ''
)

const timelineInsetStyle = computed(() => ({
  '--timeline-header-space': `${timelineHeaderHeight.value}px`,
  '--timeline-composer-space': `${composerHeight.value}px`
}))

let panelInsetObserver: ResizeObserver | null = null

function sampleMaskSource(input: string, count: number): string[] {
  if (!input) return []
  const chars = Array.from(input)
  if (chars.length <= count) return chars
  return Array.from({ length: count }, (_, index) => chars[Math.floor((index * chars.length) / count)] ?? chars[chars.length - 1])
}

function mosaicColor(char: string, index: number): string {
  const seed = char.charCodeAt(0) + index * 29
  const hue = seed % 360
  const lightness = 44 + (seed % 14)
  return `hsl(${hue}deg 64% ${lightness}%)`
}

function mosaicChipStyle(char: string, index: number): Record<string, string> {
  const base = mosaicColor(char, index)
  const opacity = `${0.74 + ((char.charCodeAt(0) + index) % 18) / 100}`
  return {
    backgroundColor: base,
    opacity
  }
}

function scrollTimelineToBottom(): void {
  if (!timelineEl.value) return
  timelineEl.value.scrollTop = timelineEl.value.scrollHeight
}

function syncTimelineInsets(): void {
  timelineHeaderHeight.value = timelineHeaderEl.value?.offsetHeight ?? 0
  composerHeight.value = composerEl.value?.offsetHeight ?? 0
}

async function bindTimelineInsetObserver(): Promise<void> {
  await nextTick()
  syncTimelineInsets()
  if (typeof ResizeObserver === 'undefined') return
  panelInsetObserver?.disconnect()
  panelInsetObserver = new ResizeObserver(() => {
    syncTimelineInsets()
  })
  if (timelineHeaderEl.value) panelInsetObserver.observe(timelineHeaderEl.value)
  if (composerEl.value) panelInsetObserver.observe(composerEl.value)
}

function conversationButtonClass(id: string): string {
  return selectedConversationId.value === id ? 'conversation-row plain active' : 'conversation-row plain'
}

async function handleCreateGroup(): Promise<void> {
  const created = await store.createGroup()
  if (created) {
    store.selectConversation(`group:${created.group_id}`)
    store.setActiveView('messages')
  }
}

async function handleSendDirect(): Promise<void> {
  const peerID = directPeerID.value
  const message = directDraft.value.trim()
  if (!peerID || !message) return
  const ok = await store.sendDirectMessage(peerID, message)
  if (!ok) return
  directDraft.value = ''
  store.selectConversation(`direct:${peerID}`)
}

async function handleSendGroup(): Promise<void> {
  const groupID = selectedGroup.value?.group_id
  const message = groupDraft.value.trim()
  if (!groupID || !message) return
  const ok = await store.sendGroupMessage(groupID, message)
  if (!ok) return
  groupDraft.value = ''
}

async function handleInvite(): Promise<void> {
  const groupID = selectedGroup.value?.group_id
  const peerID = groupInvitePeerID.value.trim()
  if (!groupID || !peerID) return
  const ok = await store.inviteToGroup(groupID, peerID)
  if (!ok) return
  groupInvitePeerID.value = ''
  showInviteDialog.value = false
}

async function handleResolveInvite(groupID: string, inviteID: string, action: 'accept' | 'reject'): Promise<void> {
  await store.resolveInvite(groupID, inviteID, action)
}

watch(
  () => selectedConversation.value?.kind,
  () => {
    void bindTimelineInsetObserver()
  },
  { immediate: true }
)

watch(
  selectedConversationId,
  async () => {
    await nextTick()
    scrollTimelineToBottom()
  },
  { flush: 'post' }
)

watch(
  () => ({
    conversationID: selectedConversationId.value,
    length: selectedTimeline.value.length,
    lastID: selectedTimeline.value.at(-1)?.id ?? ''
  }),
  async () => {
    await nextTick()
    scrollTimelineToBottom()
  },
  { flush: 'post' }
)

onMounted(() => {
  void bindTimelineInsetObserver()
  nextTick(() => {
    scrollTimelineToBottom()
  })
})

onBeforeUnmount(() => {
  panelInsetObserver?.disconnect()
  panelInsetObserver = null
})
</script>

<template>
  <section class="messages-panel">
    <aside class="message-sidebar">
      <div class="conversation-list">
        <div class="conversation-list-head">
          <div class="toolbar">
            <QButton class="primary" @click="handleCreateGroup">+ Group</QButton>
            <QButton class="outlined" :disabled="loading" @click="store.refreshAll">Refresh</QButton>
          </div>

          <div v-if="pendingInvites.length" class="invite-list">
            <h3 class="section-title">Pending Invites</h3>
            <div v-for="invite in pendingInvites" :key="invite.invite_id" class="invite-card frame">
              <div class="invite-copy">
                <code class="row-title">{{ invite.group_id }}</code>
                <span class="muted">{{ invite.inviter_peer_id }}</span>
              </div>
              <div class="invite-actions">
                <QButton class="primary sm" @click="handleResolveInvite(invite.group_id, invite.invite_id, 'accept')">
                  Accept
                </QButton>
                <QButton class="outlined sm" @click="handleResolveInvite(invite.group_id, invite.invite_id, 'reject')">
                  Reject
                </QButton>
              </div>
            </div>
          </div>
        </div>

        <div
          v-for="conversation in orderedConversations"
          :key="conversation.id"
          :class="conversationButtonClass(conversation.id)"
          role="button"
          tabindex="0"
          :aria-current="selectedConversationId === conversation.id ? 'true' : undefined"
          @click="store.selectConversation(conversation.id)"
          @keydown.enter.prevent="store.selectConversation(conversation.id)"
          @keydown.space.prevent="store.selectConversation(conversation.id)"
        >
          <div class="conversation-copy">
            <div class="conversation-head">
              <strong>{{ conversation.title }}</strong>
              <QBadge v-if="conversation.unread_count > 0" type="primary" size="sm" variant="light" rounded>
                {{ conversation.unread_count }}
              </QBadge>
            </div>
            <span class="muted">{{ conversation.last_preview || 'No messages yet' }}</span>
            <span class="meta">{{ formatTimestamp(conversation.updated_at) }}</span>
          </div>
        </div>
      </div>
    </aside>

    <section class="timeline-panel" :style="timelineInsetStyle">
      <div ref="timelineHeaderEl" class="timeline-header frame flat">
        <div>
          <h2 class="title timeline-title" :title="selectedConversation?.title ?? 'Messages'">
            <template v-if="timelineHeaderGroupTitle">
              <span class="group-title-prefix">{{ timelineHeaderGroupTitle.prefix }}</span>
              <span class="group-title-mask" aria-hidden="true">
                <span
                  v-for="chip in timelineHeaderGroupTitle.chips"
                  :key="chip.key"
                  class="group-title-chip"
                  :style="chip.style"
                />
              </span>
              <span v-if="timelineHeaderGroupTitle.suffix" class="group-title-suffix">{{ timelineHeaderGroupTitle.suffix }}</span>
            </template>
            <template v-else>
              {{ selectedConversation?.title ?? 'Messages' }}
            </template>
          </h2>
        </div>
        <div v-if="selectedGroup" class="header-actions">
          <QButton class="primary sm header-icon-button icon" aria-label="Add member" @click="showInviteDialog = true">
            <QIconPlus class="icon"/>
          </QButton>
        </div>
      </div>

      <div ref="timelineEl" class="timeline">
        <article
          v-for="message in selectedTimeline"
          :key="message.id"
          class="bubble"
          :class="message.direction"
        >
          <div class="bubble-frame frame">
            <div class="bubble-meta">
              <span class="bubble-author">
                {{ message.direction === 'inbound' ? store.labelForPeer(message.peer_id ?? '') : 'You' }}
              </span>
              <span class="muted-text">{{ formatTimestamp(message.timestamp) }}</span>
            </div>
            <div class="bubble-body">{{ message.text }}</div>
            <div class="bubble-foot">
              <code class="topic-code">{{ message.topic }}</code>
              <QButton
                v-if="message.direction === 'inbound' && message.read === false"
                class="plain xs"
                @click="store.markMessageRead(message.id)"
              >
                Mark Read
              </QButton>
            </div>
          </div>
        </article>
        <QFence v-if="!selectedTimeline.length" class="timeline-empty" text="No messages yet." />
      </div>

      <div v-if="selectedConversation?.kind === 'direct'" ref="composerEl" class="composer frame flat">
        <QTextarea
          class="composer-textarea"
          v-model="directDraft"
          rows="1"
          placeholder="Type a direct message..."
        >
          <template #append>
            <div class="composer-send-wrap">
              <QButton class="primary sm composer-send icon" aria-label="Send direct message" @click="handleSendDirect">
                <QIconSend class="icon" />
              </QButton>
            </div>
          </template>
        </QTextarea>
      </div>

      <div v-else-if="selectedConversation?.kind === 'group'" ref="composerEl" class="composer frame flat">
        <QTextarea
          class="composer-textarea"
          v-model="groupDraft"
          rows="1"
          placeholder="Type a group message..."
        >
          <template #append>
            <div class="composer-send-wrap">
              <QButton class="primary sm composer-send icon" aria-label="Send group message" @click="handleSendGroup">
                <QIconSend class="icon" />
              </QButton>
            </div>
          </template>
        </QTextarea>
      </div>

      <QDialog v-model="showInviteDialog" title="Invite Member">
        <div class="invite-dialog-body">
          <QInput v-model="groupInvitePeerID" placeholder="Peer ID" />
          <div class="invite-dialog-actions">
            <QButton class="outlined" @click="showInviteDialog = false">Cancel</QButton>
            <QButton class="primary" @click="handleInvite">Invite</QButton>
          </div>
        </div>
      </QDialog>
    </section>
  </section>
</template>

<style scoped>
.messages-panel {
  display: grid;
  grid-template-columns: 320px minmax(0, 1fr);
  min-height: 100%;
  height: 100%;
  background: rgba(255, 255, 255, 0.18);
}

.message-sidebar,
.timeline-panel {
  min-height: 0;
  height: 100%;
  overflow: hidden;
}

.message-sidebar {
  border-right: 1px solid var(--line-soft);
}

.timeline-panel {
  position: relative;
}

.toolbar,
.composer-actions,
.header-actions,
.bubble-meta,
.bubble-foot,
.invite-actions {
  display: flex;
  gap: 10px;
  align-items: center;
}

.timeline-header {
  position: absolute !important;
  top: 0;
  left: 0;
  right: 0;
  box-sizing: border-box;
  z-index: 2;
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: center;
  height: 56px;
  min-height: 56px;
  max-height: 56px;
  padding: 8px;
  border-bottom: 1px solid var(--line-soft);
}

.timeline-header .title {
  font-size: 15px;
  letter-spacing: 0.08em;
}

.header-icon-button {
  min-width: 32px;
  width: 32px;
  height: 32px;
  padding: 0;
}

.timeline-title {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.group-title-prefix,
.group-title-suffix {
  white-space: nowrap;
}

.group-title-mask {
  display: grid;
  grid-template-rows: repeat(2, 5px);
  grid-auto-flow: column;
  grid-auto-columns: 6px;
  gap: 2px;
  align-items: center;
}

.group-title-chip {
  width: 100%;
  height: 100%;
  border-radius: 1px;
  box-shadow:
    0 0 0 0.5px rgba(0, 0, 0, 0.08),
    inset 0 1px rgba(255, 255, 255, 0.28);
}

.conversation-list {
  height: 100%;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  display: grid;
  align-content: start;
}

.conversation-list-head {
  position: sticky;
  top: 0;
  z-index: 1;
  display: grid;
  gap: 12px;
  padding: 12px;
  background: linear-gradient(180deg, rgba(247, 242, 234, 0.98), rgba(247, 242, 234, 0.94));
  border-bottom: 1px solid var(--line-soft);
}

.invite-list {
  display: grid;
  gap: 8px;
}

.invite-card {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 12px;
  align-items: center;
  padding: 10px 12px;
}

.invite-copy {
  min-width: 0;
  display: grid;
  gap: 2px;
}

.conversation-copy {
  min-width: 0;
  display: grid;
  gap: 4px;
  padding: 12px;
  text-align: left;
  overflow: hidden;
}

.conversation-row {
  width: 100%;
  display: block;
  padding: 0;
  border: 0;
  border-bottom: 1px solid var(--line-soft);
  background: transparent;
  cursor: pointer;
}

.conversation-row:focus-visible {
  outline: 1px solid rgba(217, 106, 43, 0.45);
  outline-offset: -1px;
}

.conversation-row.active {
  background: rgba(217, 106, 43, 0.12);
}

.timeline {
  height: 100%;
  overflow: auto;
  display: grid;
  align-content: start;
  gap: 12px;
  padding: calc(var(--timeline-header-space, 0px) + 16px) 16px calc(var(--timeline-composer-space, 0px) + 16px);
}

.timeline {
  scrollbar-gutter: stable;
}

.bubble.inbound {
  justify-self: start;
}

.bubble.outbound {
  justify-self: end;
}

.bubble {
  width: min(720px, 100%);
}

.bubble-frame {
  display: grid;
  gap: 10px;
  padding: 12px 14px;
  color: var(--text-0);
}

.bubble.inbound .bubble-frame {
  border-left: 2px solid rgba(52, 52, 52, 0.25);
}

.bubble.outbound .bubble-frame {
  border-left: 2px solid rgba(217, 106, 43, 0.45);
}

.bubble-meta,
.bubble-foot {
  justify-content: space-between;
}

.bubble-body {
  display: block;
  min-height: 1lh;
  color: var(--text-0);
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}

.bubble-author {
  font-weight: 600;
}

.meta,
.bubble-foot {
  color: var(--text-2);
  font-size: 11px;
}

.conversation-head {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
  min-width: 0;
}

.conversation-head strong,
.conversation-copy .muted,
.conversation-copy .meta {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.conversation-head strong,
.conversation-copy .muted {
  white-space: nowrap;
}

.conversation-head strong {
  flex: 1 1 auto;
}

.section-title,
.row-title,
.topic-code {
  margin: 0;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--text-1);
}

.section-title {
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.invite-dialog-body {
  display: grid;
  gap: 12px;
  padding-top: 8px;
}

.invite-dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}

.timeline-empty {
  align-self: start;
}

.composer {
  position: absolute !important;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 2;
  display: grid;
  gap: 10px;
  padding: 12px 16px 16px;
  border-top: 1px solid var(--line-soft);
}

.composer-send {
  min-width: 36px;
  width: 36px;
  height: 36px;
  padding: 0;
}

.composer-send-wrap {
  display: flex;
  align-items: center;
  height: 100%;
  padding: 8px;
}

:deep(.composer-textarea .q-textarea-wrapper) {
  min-height: 54px;
  height: 54px;
}

:deep(.composer-textarea .q-textarea-inner) {
  align-items: center;
}

:deep(.composer-textarea textarea) {
  min-height: 54px !important;
  height: 54px !important;
  padding-top: 14px;
  padding-bottom: 14px;
}

@media (max-width: 960px) {
  .messages-panel {
    grid-template-columns: 1fr;
  }

  .message-sidebar {
    border-right: 0;
    border-bottom: 1px solid var(--line-soft);
    max-height: 40vh;
  }

  .timeline-header {
    flex-direction: column;
  }

  .invite-card {
    grid-template-columns: 1fr;
  }

  .invite-dialog-actions,
  .composer-actions {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
