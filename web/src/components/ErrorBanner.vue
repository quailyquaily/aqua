<script setup lang="ts">
defineProps<{
  text: string
}>()

defineEmits<{
  dismiss: []
}>()
</script>

<template>
  <div class="error-banner" role="alert" aria-live="assertive">
    <QFence :text="text" type="error" />
    <QButton class="plain danger xs" aria-label="Dismiss error message" @click="$emit('dismiss')">
      Dismiss
    </QButton>
  </div>
</template>

<style scoped>
.error-banner {
  margin-top: 16px;
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: flex-start;
  padding: 0 20px;
}

.error-banner :deep(.q-fence) {
  flex: 1 1 320px;
}

@media (max-width: 960px) {
  .error-banner {
    padding: 0 12px;
  }
}
</style>
