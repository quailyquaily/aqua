<script setup lang="ts">
defineProps<{
  text: string
}>()

defineEmits<{
  dismiss: []
}>()
</script>

<template>
  <div class="error-banner">
    <QFence :text="text" type="error" />
    <QButton class="plain danger xs" @click="$emit('dismiss')">Dismiss</QButton>
  </div>
</template>

<style scoped>
.error-banner {
  margin-top: 16px;
  display: flex;
  gap: 12px;
  align-items: flex-start;
}
</style>
