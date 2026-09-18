<template>
  <div class="trace-answer">
    <p v-if="!completed" class="trace-answer__note">{{ t('sessionManagement.partialAnswer') }}</p>
    <p v-if="fallback" class="trace-answer__note">{{ t('sessionManagement.fallbackAnswer') }}</p>
    <pre v-if="content" tabindex="0">{{ content }}</pre>
    <p v-else class="trace-answer__note">{{ t('sessionManagement.noAnswer') }}</p>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{ content?: string; completed?: boolean; fallback?: boolean }>()
const { t } = useI18n()
</script>
<style scoped>
.trace-answer { min-width: 0; }
pre { max-height: 560px; overflow: auto; overscroll-behavior: contain; white-space: pre-wrap; overflow-wrap: anywhere; margin: 0; padding: 16px; border: 1px solid var(--td-component-stroke); border-radius: var(--app-radius-sm); font-family: inherit; font-size: var(--app-text-base); line-height: 1.75; background: var(--td-bg-color-container); }
.trace-answer__note { color: var(--td-text-color-secondary); font-size: var(--app-text-md); }
</style>
