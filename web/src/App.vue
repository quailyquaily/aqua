<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'
import { storeToRefs } from 'pinia'
import AppTopBar from './components/AppTopBar.vue'
import ErrorBanner from './components/ErrorBanner.vue'
import SidebarNav from './components/SidebarNav.vue'
import ContactsPage from './pages/ContactsPage.vue'
import MessagesPage from './pages/MessagesPage.vue'
import NodePage from './pages/NodePage.vue'
import SettingsPage from './pages/SettingsPage.vue'
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
    <AppTopBar />
    <div class="workspace">
      <SidebarNav />

      <main class="content">
        <QProgress v-if="loading" :infinite="true" />
        <ErrorBanner v-if="errorText" :text="errorText" @dismiss="store.clearError" />
        <div v-show="activeView === 'messages'">
          <MessagesPage />
        </div>
        <div v-show="activeView === 'contacts'">
          <ContactsPage />
        </div>
        <div v-show="activeView === 'node'">
          <NodePage />
        </div>
        <div v-show="activeView === 'settings'">
          <SettingsPage />
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
  padding-top: var(--topbar-height);
}

.workspace {
  flex: 1 1 auto;
  display: grid;
  grid-template-columns: 210px minmax(0, 1fr);
  align-items: stretch;
  gap: 0;
  height: 100%;
  min-height: calc(100vh - var(--app-shell-vpad) - var(--topbar-height));
  min-height: calc(100dvh - var(--app-shell-vpad) - var(--topbar-height));
}

.content {
  padding: 10px 12px;
  min-width: 0;
  min-height: 100%;
}

@media (max-width: 960px) {
  .workspace {
    grid-template-columns: 1fr;
    min-height: auto;
  }

  .content {
    padding: 10px;
  }
}
</style>
