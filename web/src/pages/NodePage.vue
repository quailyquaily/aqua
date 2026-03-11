<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { useConsoleStore } from '../stores/console'

const store = useConsoleStore()
const { loading, runtime } = storeToRefs(store)
</script>

<template>
  <section class="node-page">
    <div class="toolbar header-row">
      <h2 class="title">Node</h2>
      <QButton class="outlined" :disabled="loading" @click="store.refreshAll">Refresh</QButton>
    </div>

    <div class="node-grid">
      <section class="node-section frame">
        <h3 class="stat-group-title">Runtime</h3>
        <div class="info-grid">
          <div class="info-item">
            <div class="info-label">Status</div>
            <div class="info-value">{{ runtime?.status || 'unknown' }}</div>
          </div>
          <div class="info-item">
            <div class="info-label">Version</div>
            <div class="info-value">{{ runtime?.version || '-' }}</div>
          </div>
          <div class="info-item">
            <div class="info-label">Started</div>
            <div class="info-value">{{ runtime?.started_at || '-' }}</div>
          </div>
          <div class="info-item">
            <div class="info-label">Web Console</div>
            <div class="info-value">{{ runtime?.web_console_addr || '-' }}</div>
          </div>
          <div class="info-item">
            <div class="info-label">Static Dir</div>
            <div class="info-value">{{ runtime?.console_web_dst_dir || 'API-only mode' }}</div>
          </div>
          <div class="info-item">
            <div class="info-label">Unread</div>
            <div class="info-value">{{ runtime?.unread_count ?? 0 }}</div>
          </div>
          <div class="info-item">
            <div class="info-label">Contacts / Groups</div>
            <div class="info-value">{{ runtime?.contacts_count ?? 0 }} / {{ runtime?.groups_count ?? 0 }}</div>
          </div>
        </div>
      </section>

      <section class="node-section frame">
        <h3 class="stat-group-title">Identity</h3>
        <div class="info-grid">
          <div class="info-item">
            <div class="info-label">Nickname</div>
            <div class="info-value">{{ runtime?.node.nickname || '-' }}</div>
          </div>
          <div class="info-item info-item-wide">
            <div class="info-label">Peer ID</div>
            <div class="info-value mono">{{ runtime?.node.peer_id || '-' }}</div>
          </div>
          <div class="info-item info-item-wide">
            <div class="info-label">Node UUID</div>
            <div class="info-value mono">{{ runtime?.node.node_uuid || '-' }}</div>
          </div>
          <div class="info-item info-item-wide">
            <div class="info-label">Node ID</div>
            <div class="info-value mono">{{ runtime?.node.node_id || '-' }}</div>
          </div>
          <div class="info-item info-item-wide">
            <div class="info-label">Identity Key</div>
            <div class="info-value mono">{{ runtime?.node.identity_pub_ed25519 || '-' }}</div>
          </div>
        </div>
      </section>

      <section class="node-section frame">
        <h3 class="stat-group-title">Addresses</h3>
        <div class="list-block">
          <div>
            <div class="list-title">Listen</div>
            <pre class="node-pre">{{ runtime?.listen_addrs?.join('\n') || '-' }}</pre>
          </div>
          <div>
            <div class="list-title">Advertised</div>
            <pre class="node-pre">{{ runtime?.addresses?.join('\n') || '-' }}</pre>
          </div>
        </div>
      </section>

      <section class="node-section frame">
        <h3 class="stat-group-title">Relay</h3>
        <div class="info-grid">
          <div class="info-item">
            <div class="info-label">Mode</div>
            <div class="info-value">{{ runtime?.relay_mode || '-' }}</div>
          </div>
          <div class="info-item">
            <div class="info-label">Probe</div>
            <div class="info-value">{{ runtime?.relay_probe ? 'enabled' : 'disabled' }}</div>
          </div>
          <div class="info-item info-item-wide">
            <div class="info-label">Relay Addrs</div>
            <pre class="node-pre inline-pre">{{ runtime?.relay_addrs?.join('\n') || '-' }}</pre>
          </div>
        </div>
        <div class="list-title">Recent Events</div>
        <pre class="node-pre">{{ runtime?.recent_relay_events?.length ? JSON.stringify(runtime.recent_relay_events, null, 2) : '[]' }}</pre>
      </section>

    </div>
  </section>
</template>

<style scoped>
.node-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
  height: 100%;
  min-height: 0;
  overflow: auto;
  padding: 18px 20px 20px;
}

.header-row {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: center;
}

.node-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
  align-items: start;
}

.node-section {
  display: grid;
  gap: 10px;
  padding: 14px 16px 16px;
  min-width: 0;
}

.node-section-wide {
  grid-column: 1 / -1;
}

.list-block {
  display: grid;
  gap: 12px;
}

.info-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px 16px;
}

.info-item {
  display: grid;
  gap: 6px;
  min-width: 0;
}

.info-item-wide {
  grid-column: 1 / -1;
}

.info-label {
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 700;
  color: var(--text-2);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.info-value {
  min-width: 0;
  font-family: var(--font-mono);
  font-size: var(--mono-size);
  line-height: 1.5;
  color: var(--text-1);
  word-break: break-word;
}

.list-title {
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 700;
  color: var(--text-2);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.node-pre {
  margin-top: 6px;
  padding: 10px;
  background: rgba(255, 255, 255, 0.46);
  border: 1px solid var(--line-soft);
  color: var(--text-1);
  overflow: auto;
}

.inline-pre {
  margin-top: 0;
}

@media (max-width: 960px) {
  .header-row {
    flex-direction: column;
    align-items: stretch;
  }

  .node-grid {
    grid-template-columns: 1fr;
  }

  .node-section-wide {
    grid-column: auto;
  }

  .info-grid {
    grid-template-columns: 1fr;
  }

  .info-item-wide {
    grid-column: auto;
  }
}
</style>
