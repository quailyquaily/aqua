<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { useConsoleStore } from '../stores/console'

const store = useConsoleStore()
const { loading, runtime } = storeToRefs(store)
</script>

<template>
  <section class="panel-surface panel">
    <div class="toolbar header-row">
      <h2 class="title">Settings</h2>
      <QButton class="outlined" :disabled="loading" @click="store.refreshAll">Refresh</QButton>
    </div>

    <QFence text="Settings are read-only in v1. Runtime values come from the current serve process." />

    <div class="stat-grid">
      <div class="stat-card">
        <h3 class="stat-group-title">Runtime</h3>
        <pre>{{ runtime }}</pre>
      </div>
      <div class="stat-card">
        <h3 class="stat-group-title">Build</h3>
        <pre>{{ runtime?.version }} / {{ runtime?.commit }} / {{ runtime?.date }}</pre>
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
