<script setup lang="ts">
import { computed } from 'vue'
import { storeToRefs } from 'pinia'
import type { ViewName } from '../types'
import { useConsoleStore } from '../stores/console'

const store = useConsoleStore()
const { activeView, wsState } = storeToRefs(store)

const items: Array<{ id: ViewName; title: string; icon: string }> = [
  { id: 'messages', title: 'Messages', icon: 'QIconMessageChatSquare' },
  { id: 'contacts', title: 'Contacts', icon: 'QIconUsers' },
  { id: 'node', title: 'Node', icon: 'QIconCpuChip' }
]

const connectionState = computed(() => {
  if (wsState.value === 'open') return { label: 'Live', tone: 'status-pill-ok' }
  if (wsState.value === 'connecting') return { label: 'Syncing', tone: 'status-pill-warn' }
  return { label: 'Offline', tone: 'status-pill-danger' }
})

function navClass(id: ViewName): string {
  return activeView.value === id ? 'nav-btn nav-btn-active' : 'nav-btn'
}
</script>

<template>
  <aside class="sidebar">
    <div class="sidebar-brand">
      <div class="brand-copy">
        <div class="brand-title">AQUA</div>
        <div class="brand-subtitle">Operator Console</div>
      </div>
      <div class="status-pill" :class="connectionState.tone" aria-live="polite">
        {{ connectionState.label }}
      </div>
    </div>

    <nav class="sidebar-nav" aria-label="Primary">
      <button
        v-for="item in items"
        :key="item.id"
        type="button"
        :class="navClass(item.id)"
        :aria-current="activeView === item.id ? 'page' : undefined"
        :title="item.title"
        @click="store.setActiveView(item.id)"
      >
        <component :is="item.icon" class="nav-icon" />
        <span class="nav-label">{{ item.title }}</span>
      </button>
    </nav>
  </aside>
</template>

<style scoped>
.sidebar {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 12px 8px 8px 0;
  border-right: 1px solid var(--line);
  height: 100%;
  min-height: 100%;
}

.sidebar-brand {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 0 8px 8px 8px;
  border-bottom: 1px solid var(--line-soft);
}

.brand-copy {
  min-width: 0;
  display: grid;
  gap: 2px;
}

.brand-title {
  font-family: var(--font-mono);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.14em;
  color: var(--text-1);
}

.brand-subtitle {
  color: var(--text-2);
  font-size: 12px;
}

.sidebar-nav {
  display: grid;
  gap: 4px;
  min-height: 0;
  flex: 1 1 auto;
  overflow-y: auto;
  align-content: start;
  padding: 4px 0;
}

.nav-btn {
  appearance: none;
  display: flex;
  align-items: center;
  gap: 8px;
  justify-content: flex-start;
  width: 100%;
  min-height: 44px;
  text-align: left;
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.2;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  padding: 9px 9px;
  color: var(--text-1);
  border: 0.5px solid transparent;
  border-radius: 0;
  background: transparent;
  cursor: pointer;
  user-select: none;
  transition:
    background-color 180ms var(--ease-out-quart),
    color 180ms var(--ease-out-quart),
    box-shadow 180ms var(--ease-out-quart),
    border-color 180ms var(--ease-out-quart),
    transform 180ms var(--ease-out-quart);
}

.nav-btn:focus-visible {
  outline: 1px solid color-mix(in srgb, var(--accent-1) 45%, transparent);
  outline-offset: -1px;
}

.nav-btn:not(.nav-btn-active):hover,
.nav-btn:not(.nav-btn-active):focus-visible {
  background-color: rgba(0, 0, 0, 0.06);
  color: var(--text-0);
  transform: translateX(2px);
}

.nav-btn.nav-btn-active {
  background: rgba(217, 106, 43, 0.12);
  border-color: rgba(183, 88, 34, 0.24);
  color: var(--accent-1);
  font-weight: 700;
}

.nav-label {
  min-width: 0;
}

.nav-icon {
  width: 16px;
  height: 16px;
  flex: 0 0 16px;
}

@media (max-width: 960px) {
  .sidebar {
    gap: 10px;
    padding: 12px 12px 0;
    border-right: 0;
    border-bottom: 1px solid var(--line);
    height: auto;
    min-height: auto;
  }

  .sidebar-brand {
    padding: 0 0 10px;
  }

  .sidebar-nav {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
    overflow: visible;
    padding: 0 0 12px;
  }

  .nav-btn {
    justify-content: center;
    min-height: 48px;
    padding: 12px 10px;
  }

  .nav-label {
    white-space: nowrap;
  }
}
</style>
