<script setup lang="ts">
import { ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useConsoleStore } from '../stores/console'
import { initialForLabel } from '../utils/messages'

const store = useConsoleStore()
const { contacts, loading } = storeToRefs(store)

const showAddDialog = ref(false)
const contactAddress = ref('')
const contactDisplayName = ref('')
const contactVerify = ref(false)

async function handleAddContact(): Promise<void> {
  const address = contactAddress.value.trim()
  if (!address) return
  const ok = await store.addContactFromAddress(address, contactDisplayName.value.trim(), contactVerify.value)
  if (!ok) return
  contactAddress.value = ''
  contactDisplayName.value = ''
  contactVerify.value = false
  showAddDialog.value = false
}
</script>

<template>
  <section class="panel panel-surface">
    <div class="toolbar header-row">
      <h2 class="title">Contacts</h2>
      <div class="header-actions">
        <QButton class="outlined" :disabled="loading" @click="store.refreshAll">Refresh</QButton>
        <QButton class="primary" @click="showAddDialog = true">Add Contact</QButton>
      </div>
    </div>

    <QFence text="Fetch by address requires the peer to be online and reachable." type="warning" />

    <table class="data-table">
      <thead>
        <tr>
          <th>Name</th>
          <th>Peer ID</th>
          <th>Trust</th>
          <th>Addresses</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="contact in contacts" :key="contact.peer_id">
          <td class="name-cell">
            <QAvatar :size="34" border-type="solid">
              <span class="avatar-initial">
                {{ initialForLabel(contact.display_name || contact.nickname || store.labelForPeer(contact.peer_id)) }}
              </span>
            </QAvatar>
            <div class="contact-copy">
              <strong>{{ contact.display_name || contact.nickname || store.labelForPeer(contact.peer_id) }}</strong>
              <span class="muted">{{ contact.nickname || 'No remote nickname' }}</span>
            </div>
          </td>
          <td><code class="mono">{{ contact.peer_id }}</code></td>
          <td>
            <QBadge
              :type="contact.trust_state === 'verified' ? 'success' : contact.trust_state === 'tofu' ? 'warning' : 'danger'"
              variant="light"
              rounded
            >
              {{ contact.trust_state }}
            </QBadge>
          </td>
          <td>{{ contact.addresses.join(', ') }}</td>
        </tr>
      </tbody>
    </table>

    <QDialog v-model="showAddDialog" title="Add Contact">
      <div class="dialog-body">
        <QInput v-model="contactAddress" placeholder="Peer multiaddr" />
        <QInput v-model="contactDisplayName" placeholder="Display name" />
        <div class="switch-row">
          <span>Verify on import</span>
          <QSwitch v-model="contactVerify" />
        </div>
        <div class="dialog-actions">
          <QButton class="outlined" @click="showAddDialog = false">Cancel</QButton>
          <QButton class="primary" @click="handleAddContact">Add Contact</QButton>
        </div>
      </div>
    </QDialog>
  </section>
</template>

<style scoped>
.panel {
  padding: 0;
}

.header-row,
.header-actions {
  display: flex;
  gap: 10px;
  align-items: center;
}

.header-row {
  justify-content: space-between;
  margin: 0 0 12px;
}

.name-cell {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 10px;
  align-items: center;
}

.contact-copy {
  display: grid;
  gap: 4px;
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

.dialog-body {
  display: grid;
  gap: 12px;
  padding-top: 8px;
}

.mono {
  font-family: var(--font-mono);
  font-size: 11px;
}

.switch-row,
.dialog-actions {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
}

@media (max-width: 960px) {
  .header-row,
  .header-actions,
  .dialog-actions {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
