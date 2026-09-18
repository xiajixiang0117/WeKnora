<template>
  <div class="trace-candidates">
    <p v-if="!candidates?.length" class="trace-candidates__empty">{{ t('sessionManagement.noChunks') }}</p>
    <details v-for="(candidate, index) in candidates" :key="`${candidate.chunk_id}:${index}`" class="trace-candidates__item">
      <summary>
        <span class="trace-candidates__title">{{ candidate.knowledge_name || candidate.knowledge_id || candidate.chunk_id }}</span>
        <span v-if="scores" class="trace-candidates__scores">
          {{ t('sessionManagement.columns.retrievalScore') }} {{ score(candidate.retrieval_score) }}
          <template v-if="scores === 'rerank'">
            · {{ t('sessionManagement.columns.modelScore') }} {{ score(candidate.model_score) }}
            · {{ t('sessionManagement.columns.finalScore') }} {{ score(candidate.final_score) }}
            · {{ candidate.selected ? t('sessionManagement.selected') : t('sessionManagement.notSelected') }}
          </template>
        </span>
        <span class="trace-candidates__id">{{ candidate.chunk_id }}</span>
        <span class="trace-candidates__hint">{{ t('sessionManagement.viewChunk') }}</span>
      </summary>
      <div class="trace-candidates__body">
        <dl>
          <div><dt>{{ t('sessionManagement.columns.knowledgeBase') }}</dt><dd>{{ candidate.knowledge_base_name || candidate.knowledge_base_id || '--' }}</dd></div>
          <div><dt>{{ t('sessionManagement.columns.documentId') }}</dt><dd>{{ candidate.knowledge_id || '--' }}</dd></div>
          <div><dt>{{ t('sessionManagement.columns.chunk') }}</dt><dd>{{ candidate.chunk_id }}</dd></div>
        </dl>
        <p v-if="candidate.citation_match === 'document'" class="trace-candidates__empty">{{ t('sessionManagement.documentCitationNote') }}</p>
        <p v-if="candidate.content_source === 'answer_reference'" class="trace-candidates__empty">{{ t('sessionManagement.referenceContentNote') }}</p>
        <pre v-if="candidate.content" tabindex="0">{{ candidate.content }}</pre>
        <p v-else class="trace-candidates__empty">{{ t('sessionManagement.chunkUnavailable') }}</p>
      </div>
    </details>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { RetrievalTraceCandidate } from '@/api/session-usage'
defineProps<{ candidates?: RetrievalTraceCandidate[]; scores?: 'retrieval' | 'rerank' }>()
const { t } = useI18n()
function score(value?: number) { return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(4) : '--' }
</script>

<style scoped>
.trace-candidates { min-width: 0; }
.trace-candidates__item { border-top: 1px solid var(--td-component-stroke); }
summary { padding: 12px 0; cursor: pointer; overflow-wrap: anywhere; font-size: var(--app-text-md); }
.trace-candidates__title { font-weight: 500; }
.trace-candidates__scores { margin-left: 12px; color: var(--td-text-color-secondary); }
.trace-candidates__id { display: block; margin-top: 4px; color: var(--td-text-color-placeholder); font-size: var(--app-text-sm); }
.trace-candidates__hint { color: var(--td-brand-color); font-size: var(--app-text-sm); }
.trace-candidates__body { min-width: 0; padding-bottom: 12px; }
dl { margin: 0 0 10px; font-size: var(--app-text-sm); }
dl > div { display: flex; gap: 8px; margin: 4px 0; }
dt { flex: 0 0 90px; color: var(--td-text-color-secondary); }
dd { margin: 0; min-width: 0; overflow-wrap: anywhere; }
pre { max-height: 420px; overflow: auto; overscroll-behavior: contain; white-space: pre-wrap; overflow-wrap: anywhere; margin: 0; padding: 14px; background: var(--td-bg-color-secondarycontainer); border-radius: var(--app-radius-xs); font-size: var(--app-text-md); line-height: 1.65; }
.trace-candidates__empty { color: var(--td-text-color-secondary); font-size: var(--app-text-md); line-height: 1.6; }
</style>
