<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useConsoleStore } from '../stores/console'
import { formatTimestamp } from '../utils/messages'

const store = useConsoleStore()
const { contacts, loading } = storeToRefs(store)

const showAddDialog = ref(false)
const contactAddress = ref('')
const contactDisplayName = ref('')
const contactVerify = ref(false)
const addingContact = ref(false)
const copiedAddress = ref('')

const canAddContact = computed(() => contactAddress.value.trim().length > 0)

const orderedContacts = computed(() =>
  [...contacts.value].sort((a, b) => (a.updated_at < b.updated_at ? 1 : -1))
)

let copyFeedbackTimer: number | null = null

function resetAddForm(): void {
  contactAddress.value = ''
  contactDisplayName.value = ''
  contactVerify.value = false
}

function closeAddDialog(): void {
  if (addingContact.value) return
  showAddDialog.value = false
  resetAddForm()
}

async function handleAddContact(): Promise<void> {
  if (addingContact.value || !canAddContact.value) return
  const address = contactAddress.value.trim()
  addingContact.value = true
  try {
    const ok = await store.addContactFromAddress(address, contactDisplayName.value.trim(), contactVerify.value)
    if (!ok) return
    showAddDialog.value = false
    resetAddForm()
  } finally {
    addingContact.value = false
  }
}

async function handleCopy(text: string): Promise<void> {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text)
  } else {
    const input = document.createElement('textarea')
    input.value = text
    document.body.appendChild(input)
    input.select()
    document.execCommand('copy')
    document.body.removeChild(input)
  }
  copiedAddress.value = text
  if (copyFeedbackTimer !== null) window.clearTimeout(copyFeedbackTimer)
  copyFeedbackTimer = window.setTimeout(() => {
    copiedAddress.value = ''
    copyFeedbackTimer = null
  }, 1600)
}

onBeforeUnmount(() => {
  if (copyFeedbackTimer !== null) window.clearTimeout(copyFeedbackTimer)
})

watch(showAddDialog, (open) => {
  if (!open && !addingContact.value) resetAddForm()
})
</script>

<template>
  <section class="contacts-page">
    <div class="toolbar header-row">
      <h2 class="title">Contacts</h2>
      <div class="header-actions">
        <QButton class="outlined" :disabled="loading" @click="store.refreshAll">Refresh</QButton>
        <QButton class="primary" @click="showAddDialog = true">Add Contact</QButton>
      </div>
    </div>

    <QFence text="Fetch by address requires the peer to be online and reachable." type="warning" />

    <div class="contacts-list">
      <article v-for="contact in orderedContacts" :key="contact.peer_id" class="contact-item frame">
        <div class="contact-head">
          <div class="contact-identity">
            <strong class="contact-name" :title="contact.display_name || contact.nickname || contact.peer_id">
              {{ contact.display_name || contact.nickname || store.labelForPeer(contact.peer_id) }}
            </strong>
            <div class="contact-trust-row">
              <QBadge
                :type="contact.trust_state === 'verified' ? 'success' : contact.trust_state === 'tofu' ? 'warning' : 'danger'"
                variant="light"
                rounded
              >
                {{ contact.trust_state }}
              </QBadge>
              <span class="muted">{{ formatTimestamp(contact.updated_at) }}</span>
            </div>
          </div>
          <QButton class="primary sm" @click="store.openDirectConversation(contact.peer_id)">
            Message
          </QButton>
        </div>

        <div class="contact-body">
          <div class="contact-meta">
            <div class="meta-label">Peer ID</div>
            <div class="meta-value">
              <code class="mono contact-peer">{{ contact.peer_id }}</code>
            </div>
          </div>
          <div class="contact-meta contact-meta-wide">
            <div class="meta-label">Addresses</div>
            <div class="address-list">
              <div v-for="address in contact.addresses" :key="`${contact.peer_id}-${address}`" class="address-item">
                <code class="mono address-chip">{{ address }}</code>
                <QButton
                  class="plain xs address-copy"
                  :class="{ copied: copiedAddress === address }"
                  :aria-label="copiedAddress === address ? 'Address copied' : 'Copy address'"
                  :title="copiedAddress === address ? 'Copied' : 'Copy address'"
                  @click="handleCopy(address)"
                >
                  <QIconCheckCircle v-if="copiedAddress === address" />
                  <QIconCopy v-else />
                </QButton>
              </div>
              <span v-if="!contact.addresses.length" class="muted">No known address</span>
            </div>
          </div>
        </div>
      </article>

      <div v-if="!orderedContacts.length" class="empty-state-panel frame">
        <p class="empty-state-eyebrow">Contacts</p>
        <h3 class="empty-state-title">No contacts yet</h3>
        <p class="empty-state-copy">
          Import a peer multiaddr to start direct conversations and populate the address book for this node.
        </p>
        <div class="empty-state-actions">
          <QButton class="primary" @click="showAddDialog = true">Add Contact</QButton>
        </div>
      </div>
    </div>

    <QDialog v-model="showAddDialog" title="Add Contact">
      <div class="dialog-body">
        <label class="field-block">
          <span class="field-label">Peer multiaddr</span>
          <QInput v-model="contactAddress" placeholder="/dns4/example/tcp/8088/p2p/..." />
        </label>
        <label class="field-block">
          <span class="field-label">Display name</span>
          <QInput v-model="contactDisplayName" placeholder="Optional operator label" />
        </label>
        <div class="field-block">
          <div class="switch-row">
            <div class="switch-copy">
              <span class="field-label">Verification</span>
              <span class="field-hint">Attempt trust verification during import when the peer is reachable.</span>
            </div>
            <QSwitch v-model="contactVerify" />
          </div>
        </div>
        <div class="dialog-actions">
          <QButton class="outlined" :disabled="addingContact" @click="closeAddDialog">Cancel</QButton>
          <QButton class="primary" :disabled="!canAddContact" :loading="addingContact" @click="handleAddContact">
            Add Contact
          </QButton>
        </div>
      </div>
    </QDialog>
  </section>
</template>

<style scoped>
.contacts-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
  height: 100%;
  min-height: 0;
  overflow: auto;
  padding: 18px 20px 20px;
}

.header-row,
.header-actions {
  display: flex;
  gap: 10px;
  align-items: center;
}

.header-row {
  justify-content: space-between;
  margin: 0;
}

.dialog-body {
  display: grid;
  gap: 12px;
  padding-top: 8px;
}

.contacts-list {
  display: grid;
  gap: 14px;
  min-width: 0;
  overflow: auto;
}

.contact-item {
  display: grid;
  gap: 14px;
  padding: 14px 16px 16px;
}

.contact-head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
}

.contact-identity {
  display: grid;
  gap: 6px;
  min-width: 0;
}

.contact-name {
  font-size: 15px;
  line-height: 1.3;
}

.contact-body {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 12px 16px;
}

.contact-meta {
  display: grid;
  gap: 6px;
  min-width: 0;
}

.contact-trust-row {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}

.meta-label {
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 700;
  color: var(--text-2);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.meta-value {
  min-width: 0;
  color: var(--text-1);
}

.address-list {
  display: grid;
  gap: 8px;
}

.address-item {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  align-items: center;
}

.address-chip {
  display: block;
  width: 100%;
  max-width: 100%;
  padding: 6px 8px;
  border: 1px solid var(--line-soft);
  background: rgba(255, 255, 255, 0.4);
  word-break: break-all;
}

.address-copy {
  min-width: 28px;
  width: 28px;
  height: 28px;
  padding: 0;
}

.address-copy.copied {
  color: var(--ok);
}

.contact-peer {
  color: var(--text-2);
  word-break: break-all;
}

.switch-row,
.dialog-actions {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
}

.switch-copy {
  display: grid;
  gap: 2px;
}

@media (max-width: 960px) {
  .header-row,
  .header-actions,
  .dialog-actions {
    flex-direction: column;
    align-items: stretch;
  }

  .contact-head {
    flex-direction: column;
    align-items: stretch;
  }

  .switch-row {
    align-items: flex-start;
  }
}
</style>
