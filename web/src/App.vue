<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'
import { storeToRefs } from 'pinia'
import ErrorBanner from './components/ErrorBanner.vue'
import SidebarNav from './components/SidebarNav.vue'
import ContactsPage from './pages/ContactsPage.vue'
import MessagesPage from './pages/MessagesPage.vue'
import NodePage from './pages/NodePage.vue'
import { useConsoleStore } from './stores/console'

const store = useConsoleStore()
const { activeView, errorText, loading } = storeToRefs(store)

onMounted(() => {
  store.start()
})

onBeforeUnmount(() => {
  store.stop()
})
</script>

<template>
  <div class="app-shell">
    <div class="workspace">
      <SidebarNav />

      <main class="content">
        <div class="content-progress" :class="{ 'content-progress-visible': loading }" aria-hidden="true">
          <QProgress :infinite="true" />
        </div>
        <ErrorBanner v-if="errorText" :text="errorText" @dismiss="store.clearError" />
        <div v-show="activeView === 'messages'" class="view">
          <MessagesPage />
        </div>
        <div v-show="activeView === 'contacts'" class="view">
          <ContactsPage />
        </div>
        <div v-show="activeView === 'node'" class="view">
          <NodePage />
        </div>
      </main>
    </div>
  </div>
</template>

<style scoped>
.app-shell {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100vh;
  height: 100dvh;
  min-height: 100vh;
  min-height: 100dvh;
}

.workspace {
  flex: 1 1 auto;
  display: grid;
  grid-template-columns: 210px minmax(0, 1fr);
  align-items: stretch;
  gap: 0;
  height: 100%;
  min-height: 100%;
}

.content {
  position: relative;
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}

.content-progress {
  position: absolute;
  top: 0;
  left: 20px;
  right: 20px;
  z-index: 3;
  opacity: 0;
  pointer-events: none;
  transform: translateY(-6px);
  transition:
    opacity 180ms var(--ease-out-quart),
    transform 180ms var(--ease-out-quart);
}

.content-progress-visible {
  opacity: 1;
  transform: translateY(0);
}

.content-progress :deep(.q-progress) {
  --q-progress-track-height: 3px;
  --q-progress-track-radius: 0;
  --q-progress-bar-radius: 0;
}

.view {
  flex: 1 1 auto;
  min-height: 0;
}

@media (max-width: 960px) {
  .content-progress {
    left: 12px;
    right: 12px;
  }

  .workspace {
    grid-template-columns: 1fr;
    min-height: auto;
  }
}
</style>
