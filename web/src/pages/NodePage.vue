<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { useConsoleStore } from '../stores/console'

const store = useConsoleStore()
const { loading, runtime } = storeToRefs(store)
</script>

<template>
  <section class="panel-surface panel">
    <div class="toolbar header-row">
      <h2 class="title">Node</h2>
      <QButton class="outlined" :disabled="loading" @click="store.refreshAll">Refresh</QButton>
    </div>

    <QFence text="This page is read-only in v1. Serve lifecycle stays outside the GUI." type="warning" />

    <div class="stat-grid">
      <div class="stat-card">
        <h3 class="stat-group-title">Identity</h3>
        <pre>{{ runtime?.node }}</pre>
      </div>
      <div class="stat-card">
        <h3 class="stat-group-title">Addresses</h3>
        <pre>{{ runtime?.addresses }}</pre>
      </div>
      <div class="stat-card">
        <h3 class="stat-group-title">Relay</h3>
        <pre>{{ runtime?.recent_relay_events }}</pre>
      </div>
    </div>
  </section>
</template>

<style scoped>
.panel {
  display: grid;
  gap: 12px;
}

.header-row {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: center;
}
</style>
