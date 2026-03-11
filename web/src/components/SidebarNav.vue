<script setup lang="ts">
import type { ViewName } from '../types'
import { useConsoleStore } from '../stores/console'

const store = useConsoleStore()

const items: Array<{ id: ViewName; title: string }> = [
  { id: 'messages', title: 'Messages' },
  { id: 'contacts', title: 'Contacts' },
  { id: 'node', title: 'Node' },
  { id: 'settings', title: 'Settings' }
]

function navClass(id: ViewName): string {
  return store.activeView === id ? 'nav-btn nav-btn-active' : 'nav-btn'
}
</script>

<template>
  <aside class="sidebar">
    <div class="sidebar-nav">
      <div
        v-for="item in items"
        :key="item.id"
        :class="navClass(item.id)"
        role="button"
        tabindex="0"
        :aria-current="store.activeView === item.id ? 'page' : undefined"
        @click="store.setActiveView(item.id)"
        @keydown.enter.prevent="store.setActiveView(item.id)"
        @keydown.space.prevent="store.setActiveView(item.id)"
      >
        <span class="nav-label">{{ item.title }}</span>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  padding: 8px 8px 8px 0;
  border-right: 1px solid var(--line);
  height: 100%;
  min-height: 100%;
}

.sidebar-nav {
  display: grid;
  gap: 0;
  height: calc(100% - var(--sidebar-vpad));
  overflow-y: auto;
  align-content: start;
}

.nav-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  justify-content: flex-start;
  width: 100%;
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
    background-color 0.15s ease,
    color 0.15s ease,
    box-shadow 0.15s ease,
    border-color 0.15s ease;
}

.nav-btn:focus-visible {
  outline: 1px solid var(--q-button-primary-border);
  outline-offset: 0;
}

.nav-btn:not(.nav-btn-active):hover,
.nav-btn:not(.nav-btn-active):focus-visible {
  background-color: rgba(0, 0, 0, 0.06);
  color: var(--text-0);
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
</style>
