<script setup lang="ts">
import { computed, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useConsoleStore } from '../stores/console'
import { formatTimestamp, initialForLabel } from '../utils/messages'

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

const directTargetPeerID = ref('')
const directDraft = ref('')
const groupDraft = ref('')
const groupInvitePeerID = ref('')

const directPeerID = computed(() => {
  if (selectedConversation.value?.kind === 'direct') {
    return selectedConversation.value.id.replace('direct:', '')
  }
  return directTargetPeerID.value.trim()
})

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
  directTargetPeerID.value = ''
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
}

async function handleResolveInvite(groupID: string, inviteID: string, action: 'accept' | 'reject'): Promise<void> {
  await store.resolveInvite(groupID, inviteID, action)
}
</script>

<template>
  <section class="panel-surface panel messages-panel">
    <div class="message-sidebar">
      <div class="toolbar">
        <QButton class="primary" @click="handleCreateGroup">+ Group</QButton>
        <QButton class="outlined" :disabled="loading" @click="store.refreshAll">Refresh</QButton>
      </div>

      <div v-if="pendingInvites.length" class="invite-list">
        <h3>Pending Invites</h3>
        <div v-for="invite in pendingInvites" :key="invite.invite_id" class="invite-card">
          <QAvatar :size="34" border-type="solid">
            <span class="avatar-initial">{{ initialForLabel(invite.group_id) }}</span>
          </QAvatar>
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

      <div class="conversation-list">
        <QButton
          v-for="conversation in conversations"
          :key="conversation.id"
          :class="conversationButtonClass(conversation.id)"
          @click="store.selectConversation(conversation.id)"
        >
          <QAvatar :size="38" border-type="solid">
            <span class="avatar-initial">{{ initialForLabel(conversation.title) }}</span>
          </QAvatar>
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
        </QButton>
      </div>
    </div>

    <div class="timeline-panel">
      <div class="timeline-header">
        <div>
          <h2 class="title">{{ selectedConversation?.title ?? 'Messages' }}</h2>
          <div class="muted">{{ selectedConversation?.kind ?? 'Select a conversation' }}</div>
        </div>
        <div v-if="selectedGroup" class="group-tools">
          <QInput v-model="groupInvitePeerID" placeholder="Invite peer_id" />
          <QButton class="primary" @click="handleInvite">Invite</QButton>
        </div>
      </div>

      <div class="timeline">
        <article
          v-for="message in selectedTimeline"
          :key="message.id"
          class="bubble"
          :class="message.direction"
        >
          <div class="bubble-meta">
            <div class="bubble-author">
              <QAvatar :size="26" border-type="solid">
                <span class="avatar-initial">
                  {{ initialForLabel(message.direction === 'inbound' ? store.labelForPeer(message.peer_id ?? '') : 'You') }}
                </span>
              </QAvatar>
              <span>{{ message.direction === 'inbound' ? store.labelForPeer(message.peer_id ?? '') : 'You' }}</span>
            </div>
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
        </article>
        <QFence v-if="!selectedTimeline.length" text="No messages yet." />
      </div>

      <div v-if="selectedConversation?.kind === 'direct'" class="composer">
        <QTextarea
          v-model="directDraft"
          rows="4"
          placeholder="Type a direct message..."
        />
        <div class="composer-actions">
          <QInput v-model="directTargetPeerID" placeholder="Peer ID for new direct chat" />
          <QButton class="primary" @click="handleSendDirect">Send Direct</QButton>
        </div>
      </div>

      <div v-else-if="selectedConversation?.kind === 'group'" class="composer">
        <QTextarea
          v-model="groupDraft"
          rows="4"
          placeholder="Type a group message..."
        />
        <div class="composer-actions">
          <QButton class="primary" @click="handleSendGroup">Send Group Message</QButton>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.panel {
  display: grid;
  min-height: calc(100vh - var(--topbar-height) - 20px);
}

.messages-panel {
  display: grid;
  grid-template-columns: 320px 1fr;
  gap: 12px;
}

.message-sidebar,
.timeline-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-height: 0;
}

.toolbar,
.composer-actions,
.group-tools,
.bubble-meta,
.bubble-foot,
.invite-actions {
  display: flex;
  gap: 10px;
  align-items: center;
}

.toolbar {
  margin: 0;
}

.timeline-header {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--line-soft);
}

.conversation-list,
.timeline,
.invite-list {
  display: grid;
  gap: 10px;
  overflow: auto;
}

.conversation-row,
.invite-card,
.bubble {
  position: relative;
  overflow: hidden;
  isolation: isolate;
  border: 1px solid var(--line);
  border-radius: 2px;
  background: rgba(247, 247, 247, 0.92);
  box-shadow:
    0 1px 0 rgba(255, 255, 255, 0.9) inset,
    0 6px 14px rgba(20, 24, 32, 0.055),
    0 2px 4px rgba(20, 24, 32, 0.04);
}

.invite-card {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: 8px 12px;
  align-items: center;
  padding: 8px 10px 12px;
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
  text-align: left;
}

:deep(.q-button.conversation-row) {
  width: 100%;
  height: auto;
  justify-content: flex-start;
  padding: 8px 10px 12px;
  border: 1px solid var(--line);
  border-radius: 2px;
  background: rgba(247, 247, 247, 0.92);
  box-shadow:
    0 1px 0 rgba(255, 255, 255, 0.9) inset,
    0 6px 14px rgba(20, 24, 32, 0.055),
    0 2px 4px rgba(20, 24, 32, 0.04);
}

:deep(.q-button.conversation-row .button-inner) {
  width: 100%;
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 12px;
  align-items: center;
}

:deep(.q-button.conversation-row.active) {
  border-color: rgba(183, 88, 34, 0.24);
  background: rgba(217, 106, 43, 0.12);
}

.timeline {
  flex: 1;
  min-height: 0;
}

.bubble.inbound {
  border-left: 2px solid rgba(52, 52, 52, 0.25);
}

.bubble.outbound {
  border-left: 2px solid rgba(217, 106, 43, 0.45);
}

.bubble-body {
  margin: 10px 0;
  white-space: pre-wrap;
}

.bubble-author {
  display: flex;
  gap: 8px;
  align-items: center;
}

.avatar-initial {
  display: flex;
  width: 100%;
  height: 100%;
  align-items: center;
  justify-content: center;
  font-size: 0.875rem;
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
}

.row-title,
.topic-code {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--text-1);
}

.composer {
  display: grid;
  gap: 10px;
}

@media (max-width: 960px) {
  .messages-panel {
    grid-template-columns: 1fr;
  }

  .timeline-header {
    flex-direction: column;
  }

  .invite-card {
    grid-template-columns: 1fr;
  }

  .group-tools,
  .composer-actions {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
