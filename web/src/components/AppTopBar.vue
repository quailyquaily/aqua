<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { useConsoleStore } from '../stores/console'

const store = useConsoleStore()
const { runtime, wsState } = storeToRefs(store)

function runtimeBadgeType(): string {
  return runtime.value?.status === 'running' ? 'success' : 'default'
}

function wsBadgeType(): string {
  return wsState.value === 'open' ? 'success' : wsState.value === 'connecting' ? 'warning' : 'danger'
}
</script>

<template>
  <header class="topbar">
    <div class="topbar-brand">
      <div class="brand">
        <h1 class="brand-title">Aqua Web Console</h1>
        <div class="brand-subtitle muted">Vue SPA / morph / ws {{ wsState }}</div>
      </div>
    </div>
    <div class="topbar-actions">
      <div class="status-group">
        <QBadge :type="runtimeBadgeType()" variant="light" rounded>{{ runtime?.status ?? 'unknown' }}</QBadge>
        <code class="topbar-code">{{ runtime?.node.peer_id?.slice(0, 12) ?? '...' }}</code>
      </div>
      <div class="status-group">
        <QBadge :type="wsBadgeType()" dot />
        <span class="muted">Relay {{ store.relaySummary(runtime?.recent_relay_events ?? []) }}</span>
      </div>
      <QBadge type="primary" variant="outlined" rounded>{{ runtime?.unread_count ?? 0 }} unread</QBadge>
    </div>
  </header>
</template>

<style scoped>
.topbar {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 40;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  min-height: var(--topbar-height);
  padding: 8px 12px;
  background: rgba(247, 247, 247, 0.92);
  border-bottom: 1px solid var(--line);
}

.topbar-brand {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.brand {
  min-width: 0;
}

.brand-title {
  margin: 0;
  font-family: var(--font-mono);
  font-size: 12px;
  font-weight: 700;
  line-height: 1;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--text-1);
}

.brand-subtitle {
  margin-top: 3px;
}

.topbar-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  justify-content: flex-end;
}

.status-group {
  display: flex;
  align-items: center;
  gap: 6px;
}

.topbar-code {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--text-1);
}

.muted {
  color: var(--text-2);
  font-size: 11px;
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
}

@media (max-width: 960px) {
  .topbar {
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
  }

  .topbar-actions {
    justify-content: flex-start;
    width: 100%;
  }
}
</style>
