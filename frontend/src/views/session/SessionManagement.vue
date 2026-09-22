<template>
  <main class="session-management">
    <header class="session-management__header">
      <div>
        <h1>{{ t('sessionManagement.title') }}</h1>
        <p>{{ t('sessionManagement.description') }}</p>
      </div>
      <t-tooltip :content="t('sessionManagement.refresh')">
        <t-button
          shape="square"
          variant="text"
          :loading="loading"
          :aria-label="t('sessionManagement.refresh')"
          @click="loadSessions"
        >
          <template #icon><t-icon name="refresh" /></template>
        </t-button>
      </t-tooltip>
    </header>

    <section class="session-management__toolbar" :aria-label="t('sessionManagement.searchLabel')">
      <t-input
        v-model="searchInput"
        clearable
        :placeholder="t('sessionManagement.searchPlaceholder')"
        @enter="applySearch"
        @clear="applySearch"
      >
        <template #prefix-icon><t-icon name="search" /></template>
      </t-input>
      <t-select
        v-model="source"
        :options="sourceOptions"
        :aria-label="t('sessionManagement.channelFilter')"
        @change="applySearch"
      />
      <t-button theme="primary" @click="applySearch">{{ t('sessionManagement.search') }}</t-button>
    </section>

    <section class="session-management__table-section">
      <div v-if="error" class="session-management__state session-management__state--error" role="alert">
        <t-icon name="error-circle" size="22px" />
        <span>{{ error }}</span>
        <t-button size="small" variant="outline" @click="loadSessions">{{ t('sessionManagement.retry') }}</t-button>
      </div>

      <div v-else class="data-table-shell data-table-shell--with-footer session-management__table-shell">
        <div class="data-table-shell__scroll">
          <t-table
            row-key="session_id"
            :data="rows"
            :columns="columns"
            :loading="loading"
            :empty="t('sessionManagement.empty')"
            hover
            size="medium"
          >
            <template #session="{ row }">
              <div class="session-management__session-cell">
                <span class="session-management__title">{{ row.title || t('sessionManagement.untitled') }}</span>
                <code>{{ row.session_id }}</code>
              </div>
            </template>
            <template #channel="{ row }">
              <t-tag size="small" variant="light" theme="default">{{ channelLabel(row) }}</t-tag>
            </template>
            <template #agents="{ row }">
              <div v-if="row.agents.length" class="session-management__agents">
                <span v-for="agent in row.agents" :key="agent.id" class="session-management__agent">
                  {{ agent.name || t(agent.id ? 'sessionManagement.unknownAgent' : 'sessionManagement.unrecordedAgent') }}
                </span>
              </div>
              <span v-else class="session-management__muted">{{ row.answer_count ? t('sessionManagement.unrecordedAgent') : '--' }}</span>
            </template>
            <template #answers="{ row }">
              <span>{{ row.answer_count }}</span>
              <span v-if="row.tracked_answer_count !== row.answer_count" class="session-management__muted">
                / {{ row.tracked_answer_count }}
              </span>
            </template>
            <template #input="{ row }">{{ formatTokens(row.usage.prompt_tokens) }}</template>
            <template #output="{ row }">{{ formatTokens(row.usage.completion_tokens) }}</template>
            <template #cache="{ row }">{{ formatTokens(row.usage.cache_read_tokens || 0) }}</template>
            <template #total="{ row }"><strong>{{ formatTokens(row.usage.total_tokens) }}</strong></template>
            <template #updated_at="{ row }"><time :datetime="row.updated_at">{{ formatDate(row.updated_at) }}</time></template>
            <template #actions="{ row }">
              <t-tooltip :content="t('sessionManagement.viewDetails')" placement="top">
                <t-button
                  shape="square"
                  variant="text"
                  size="small"
                  :aria-label="t('sessionManagement.viewDetails')"
                  @click="openDetails(row)"
                >
                  <template #icon><t-icon name="browse" /></template>
                </t-button>
              </t-tooltip>
            </template>
          </t-table>
        </div>
        <div v-if="total > 0" class="data-table-shell__pager">
          <t-pagination
            v-model="page"
            v-model:page-size="pageSize"
            :total="total"
            size="small"
            show-jumper
            show-page-number
            show-page-size
            :page-size-options="[20, 50, 100]"
            @change="onPageChange"
          />
        </div>
      </div>
    </section>

    <SettingDrawer
      v-model:visible="detailsVisible"
      :title="selectedSummary?.title || t('sessionManagement.detailsTitle')"
      :description="selectedSummary?.session_id || ''"
      icon="chat"
      width="1080px"
      :min-width="680"
      :max-width="1480"
      storage-key="setting-drawer:width:session-usage-details"
      hide-footer
    >
      <div v-if="detailsLoading" class="session-management__drawer-state">
        <t-loading size="small" />
        <span>{{ t('sessionManagement.loadingDetails') }}</span>
      </div>
      <div v-else-if="detailsError" class="session-management__drawer-state session-management__drawer-state--error">
        <t-icon name="error-circle" />
        <span>{{ detailsError }}</span>
      </div>
      <template v-else-if="selectedSummary">
        <section class="session-management__summary-grid" :aria-label="t('sessionManagement.summary')">
          <div><span>{{ t('sessionManagement.columns.answers') }}</span><strong>{{ selectedSummary.answer_count }}</strong></div>
          <div><span>{{ t('sessionManagement.columns.input') }}</span><strong>{{ formatTokens(selectedSummary.usage.prompt_tokens) }}</strong></div>
          <div><span>{{ t('sessionManagement.columns.output') }}</span><strong>{{ formatTokens(selectedSummary.usage.completion_tokens) }}</strong></div>
          <div><span>{{ t('sessionManagement.columns.cacheRead') }}</span><strong>{{ formatTokens(selectedSummary.usage.cache_read_tokens || 0) }}</strong></div>
          <div><span>{{ t('sessionManagement.columns.total') }}</span><strong>{{ formatTokens(selectedSummary.usage.total_tokens) }}</strong></div>
        </section>

        <div class="session-management__detail-heading">
          <h2>{{ t('sessionManagement.responseDetails') }}</h2>
          <span>{{ channelLabel(selectedSummary) }}</span>
        </div>
        <div class="data-table-shell session-management__details-table">
          <t-table row-key="message_id" :data="details" :columns="detailColumns" size="medium" hover>
            <template #created_at="{ row }"><time :datetime="row.created_at">{{ formatDate(row.created_at) }}</time></template>
            <template #question="{ row }"><span class="session-management__question-preview">{{ displayQuery(row.question || '') || '--' }}</span></template>
            <template #agent="{ row }">
              <div class="session-management__agent-cell">
                <span>{{ row.agent_name || t(row.agent_id ? 'sessionManagement.unknownAgent' : 'sessionManagement.unrecordedAgent') }}</span>
              </div>
            </template>
            <template #model="{ row }"><span v-if="row.model_name">{{ row.model_name }}</span><span v-else>--</span></template>
            <template #input="{ row }">{{ usageValue(row, 'prompt_tokens') }}</template>
            <template #output="{ row }">{{ usageValue(row, 'completion_tokens') }}</template>
            <template #cache="{ row }">{{ usageValue(row, 'cache_read_tokens') }}</template>
            <template #total="{ row }"><strong>{{ usageValue(row, 'total_tokens') }}</strong></template>
            <template #actions="{ row }"><t-button size="small" variant="text" @click="selectedAnswer = row">{{ t('sessionManagement.viewAnswer') }}</t-button></template>
          </t-table>
        </div>

        <section v-if="selectedAnswer" class="session-management__trace-detail">
          <div class="session-management__detail-heading"><h2>{{ t('sessionManagement.columns.question') }}</h2></div>
          <pre class="session-management__question-content" tabindex="0">{{ displayQuery(selectedAnswer.question || '') || '--' }}</pre>
          <div class="session-management__detail-heading"><h2>{{ t('sessionManagement.finalAnswer') }}</h2><time>{{ formatDate(selectedAnswer.created_at) }}</time></div>
          <TraceAnswer :content="selectedAnswer.content" :completed="selectedAnswer.is_completed" :fallback="selectedAnswer.is_fallback" />
        </section>

        <div class="session-management__detail-heading session-management__trace-heading">
          <h2>{{ t('sessionManagement.executionTraces') }}</h2>
        </div>
        <div v-if="tracesLoading" class="session-management__drawer-state">
          <t-loading size="small" />
          <span>{{ t('sessionManagement.loadingTraces') }}</span>
        </div>
        <div v-else-if="tracesError" class="session-management__drawer-state session-management__drawer-state--error">
          <t-icon name="error-circle" />
          <span>{{ tracesError }}</span>
        </div>
        <div v-else-if="traces.length" class="data-table-shell session-management__details-table">
          <t-table row-key="request_id" :data="traces" :columns="traceColumns" size="medium" hover>
            <template #created_at="{ row }"><time :datetime="row.created_at">{{ formatDate(row.created_at) }}</time></template>
            <template #status="{ row }"><t-tag size="small" variant="light">{{ row.status }}</t-tag></template>
            <template #query="{ row }"><span class="session-management__query">{{ displayQuery(row.original_query) }}</span></template>
            <template #actions="{ row }">
              <t-button size="small" variant="text" @click="openTrace(row)">{{ t('sessionManagement.viewTrace') }}</t-button>
            </template>
          </t-table>
        </div>
        <div v-else class="session-management__empty-trace">{{ t('sessionManagement.noTraces') }}</div>

        <div v-if="traceLoading" class="session-management__drawer-state"><t-loading size="small" />{{ t('sessionManagement.loadingTraces') }}</div>
        <div v-else-if="traceError" class="session-management__drawer-state session-management__drawer-state--error">{{ traceError }}</div>
        <section v-else-if="selectedTrace" class="session-management__trace-detail">
          <div class="session-management__detail-heading">
            <h2>{{ t('sessionManagement.traceDetail') }}</h2>
            <code>{{ selectedTrace.request_id }}</code>
          </div>
          <p class="session-management__trace-query"><span>{{ t('sessionManagement.columns.query') }}</span>{{ displayQuery(selectedTrace.trace.original_query) }}</p>
          <section class="session-management__trace-outcome">
            <h3>{{ t('sessionManagement.finalAnswer') }}</h3>
            <TraceAnswer v-if="selectedTrace.trace.answer" :content="selectedTrace.trace.answer.content" :completed="selectedTrace.trace.answer.is_completed" :fallback="selectedTrace.trace.answer.is_fallback" />
            <p v-else class="session-management__muted">{{ t('sessionManagement.answerUnavailable') }}</p>
            <h3>{{ t('sessionManagement.citedChunks') }}<template v-if="selectedTrace.trace.answer"> ({{ selectedTrace.trace.answer.cited_count }})</template></h3>
            <p class="session-management__muted">{{ t('sessionManagement.citedChunksNote') }}</p>
            <template v-if="selectedTrace.trace.answer">
              <p v-if="selectedTrace.trace.answer.truncated" class="session-management__trace-error">{{ t('sessionManagement.truncated') }}</p>
              <TraceCandidates v-if="selectedTrace.trace.answer.cited_candidates?.length" :candidates="selectedTrace.trace.answer.cited_candidates" />
              <p v-else class="session-management__muted">{{ t('sessionManagement.noCitedChunks') }}</p>
            </template>
            <p v-else class="session-management__muted">{{ t('sessionManagement.answerUnavailable') }}</p>
            <p v-if="!selectedTrace.trace.steps?.some(step => step.context)" class="session-management__muted">{{ t('sessionManagement.contextUnavailable') }}</p>
          </section>
          <details v-for="step in selectedTrace.trace.steps" :key="step.sequence" class="session-management__trace-step" open>
            <summary>
              <strong>#{{ step.sequence }} · {{ step.kind === 'generation_context' ? t('sessionManagement.generationContext') : step.kind }}<small v-if="step.source"> · {{ step.source }}</small></strong>
              <t-tag size="small" variant="light">{{ step.status }}</t-tag>
            </summary>
            <div class="session-management__trace-step-content">
              <p v-if="step.query"><span>{{ t('sessionManagement.columns.query') }}</span>{{ displayQuery(step.query) }}</p>
              <p v-if="step.rewritten_query"><span>{{ t('sessionManagement.rewrittenQuery') }}</span>{{ displayQuery(step.rewritten_query) }}</p>
              <p v-if="step.expansion_queries?.length"><span>{{ t('sessionManagement.expansionQueries') }}</span>{{ step.expansion_queries.map(displayQuery).join(' · ') }}</p>
              <p v-if="step.error_summary" class="session-management__trace-error">{{ step.error_summary }}</p>
              <template v-if="step.retrieval">
                <p>
                  <span>{{ t('sessionManagement.columns.knowledgeBase') }}</span>{{ step.retrieval.knowledge_base_name || step.retrieval.knowledge_base_id || '--' }}
                  <span>{{ t('sessionManagement.recalledCount') }}</span>{{ step.retrieval.returned_count }}
                  <span v-if="step.retrieval.truncated" class="session-management__trace-error">{{ t('sessionManagement.truncated') }}</span>
                </p>
                <TraceCandidates :candidates="step.retrieval.candidates" scores="retrieval" />
              </template>
              <template v-if="step.rerank">
                <p>
                  <span>{{ t('sessionManagement.columns.model') }}</span>{{ step.rerank.model_name || step.rerank.model_id || '--' }}
                  <span>{{ t('sessionManagement.threshold') }}</span>{{ formatScore(step.rerank.threshold) }}
                  <span v-if="step.rerank.truncated" class="session-management__trace-error">{{ t('sessionManagement.truncated') }}</span>
                </p>
                <TraceCandidates :candidates="step.rerank.candidates" scores="rerank" />
              </template>
              <template v-if="step.context">
                <p>{{ t('sessionManagement.generationContextNote') }} ({{ step.context.captured_count }} / {{ step.context.candidate_count }})</p>
                <p v-if="step.context.truncated" class="session-management__trace-error">{{ t('sessionManagement.truncated') }}</p>
                <TraceCandidates :candidates="step.context.candidates" />
              </template>
            </div>
          </details>
        </section>
      </template>
    </SettingDrawer>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import TraceCandidates from './TraceCandidates.vue'
import TraceAnswer from './TraceAnswer.vue'
import SettingDrawer from '@/components/settings/SettingDrawer.vue'
import { restoreEmbedMessageDisplay } from '@/utils/embedContext'
import {
  getSessionUsage,
  getRetrievalExecutionTrace,
  listRetrievalExecutionTraces,
  listSessionUsage,
  type RetrievalExecutionTrace,
  type RetrievalTraceSummary,
  type SessionTokenUsage,
  type SessionUsageSource,
  type SessionUsageDetail,
  type SessionUsageSummary,
} from '@/api/session-usage'

const { t, locale } = useI18n()
const rows = ref<SessionUsageSummary[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const searchInput = ref('')
const keyword = ref('')
const source = ref<SessionUsageSource | 'all'>('all')
const loading = ref(false)
const error = ref('')
const detailsVisible = ref(false)
const detailsLoading = ref(false)
const detailsError = ref('')
const selectedSummary = ref<SessionUsageSummary | null>(null)
const details = ref<SessionUsageDetail[]>([])
const traces = ref<RetrievalTraceSummary[]>([])
const tracesLoading = ref(false)
const tracesError = ref('')
const selectedTrace = ref<RetrievalExecutionTrace | null>(null)
const selectedAnswer = ref<SessionUsageDetail | null>(null)
const traceLoading = ref(false)
const traceError = ref('')
let detailRequest = 0
let traceRequest = 0

const sourceOptions = computed(() => [
  { label: t('sessionManagement.allChannels'), value: 'all' },
  ...(['web', 'embed', 'api'] as const).map((channel) => ({
    label: channelLabel({ channel }),
    value: channel,
  })),
])

const columns = computed(() => [
  { colKey: 'session', title: t('sessionManagement.columns.session'), width: 250, ellipsis: true },
  { colKey: 'channel', title: t('sessionManagement.columns.channel'), width: 112 },
  { colKey: 'agents', title: t('sessionManagement.columns.agent'), width: 168, ellipsis: true },
  { colKey: 'answers', title: t('sessionManagement.columns.answers'), width: 88, align: 'right' },
  { colKey: 'input', title: t('sessionManagement.columns.input'), width: 104, align: 'right' },
  { colKey: 'output', title: t('sessionManagement.columns.output'), width: 104, align: 'right' },
  { colKey: 'cache', title: t('sessionManagement.columns.cacheRead'), width: 112, align: 'right' },
  { colKey: 'total', title: t('sessionManagement.columns.total'), width: 104, align: 'right' },
  { colKey: 'updated_at', title: t('sessionManagement.columns.updatedAt'), width: 152 },
  { colKey: 'actions', title: '', width: 52, align: 'center' },
])

const detailColumns = computed(() => [
  { colKey: 'created_at', title: t('sessionManagement.columns.time'), width: 158 },
  { colKey: 'question', title: t('sessionManagement.columns.question'), width: 240, ellipsis: true },
  { colKey: 'agent', title: t('sessionManagement.columns.agent'), width: 220, ellipsis: true },
  { colKey: 'model', title: t('sessionManagement.columns.model'), width: 180, ellipsis: true },
  { colKey: 'input', title: t('sessionManagement.columns.input'), width: 104, align: 'right' },
  { colKey: 'output', title: t('sessionManagement.columns.output'), width: 104, align: 'right' },
  { colKey: 'cache', title: t('sessionManagement.columns.cacheRead'), width: 112, align: 'right' },
  { colKey: 'total', title: t('sessionManagement.columns.total'), width: 104, align: 'right' },
  { colKey: 'actions', title: '', width: 100, fixed: 'right' },
])

const traceColumns = computed(() => [
  { colKey: 'created_at', title: t('sessionManagement.columns.time'), width: 158 },
  { colKey: 'status', title: t('sessionManagement.columns.status'), width: 108 },
  { colKey: 'query', title: t('sessionManagement.columns.query'), ellipsis: true },
  { colKey: 'step_count', title: t('sessionManagement.columns.steps'), width: 80, align: 'right' },
  { colKey: 'actions', title: '', width: 92, align: 'center' },
])

async function loadSessions() {
  loading.value = true
  error.value = ''
  try {
    const response = await listSessionUsage({
      page: page.value,
      pageSize: pageSize.value,
      keyword: keyword.value,
      source: source.value === 'all' ? undefined : source.value,
    })
    rows.value = response.data || []
    total.value = response.total || 0
  } catch (err: any) {
    rows.value = []
    total.value = 0
    error.value = err?.message || t('sessionManagement.loadFailed')
  } finally {
    loading.value = false
  }
}

function applySearch() {
  keyword.value = searchInput.value.trim()
  page.value = 1
  void loadSessions()
}

function onPageChange(context: any) {
  page.value = context.current
  pageSize.value = context.pageSize
  void loadSessions()
}

async function openDetails(summary: SessionUsageSummary) {
  const request = ++detailRequest
  ++traceRequest
  traceLoading.value = false
  tracesLoading.value = false
  traceError.value = ''
  selectedAnswer.value = null
  selectedSummary.value = summary
  details.value = []
  traces.value = []
  tracesError.value = ''
  selectedTrace.value = null
  detailsError.value = ''
  detailsVisible.value = true
  detailsLoading.value = true
  try {
    const response = await getSessionUsage(summary.session_id)
    if (request !== detailRequest) return
    selectedSummary.value = response.data.summary
    details.value = response.data.details || []
  } catch (err: any) {
    if (request !== detailRequest) return
    detailsError.value = err?.message || t('sessionManagement.loadDetailsFailed')
  } finally {
    if (request === detailRequest) detailsLoading.value = false
  }
  if (request === detailRequest && !detailsError.value) void loadTraces(summary.session_id)
}

async function loadTraces(sessionId: string) {
  const request = detailRequest
  tracesLoading.value = true
  tracesError.value = ''
  try {
    const response = await listRetrievalExecutionTraces(sessionId)
    if (request !== detailRequest || selectedSummary.value?.session_id !== sessionId) return
    traces.value = response.data || []
  } catch (err: any) {
    if (request !== detailRequest || selectedSummary.value?.session_id !== sessionId) return
    traces.value = []
    tracesError.value = err?.message || t('sessionManagement.loadTracesFailed')
  } finally {
    if (request === detailRequest && selectedSummary.value?.session_id === sessionId) tracesLoading.value = false
  }
}

async function openTrace(summary: RetrievalTraceSummary) {
  if (!selectedSummary.value) return
  const sessionId = selectedSummary.value.session_id
  const request = ++traceRequest
  selectedTrace.value = null
  traceError.value = ''
  traceLoading.value = true
  try {
    const response = await getRetrievalExecutionTrace(sessionId, summary.request_id)
    if (request === traceRequest) selectedTrace.value = response.data
  } catch (err: any) {
    if (request === traceRequest) traceError.value = err?.message || t('sessionManagement.loadTraceFailed')
  } finally {
    if (request === traceRequest) traceLoading.value = false
  }
}

// Strip only the embed transport prefix at render time. Keep the persisted
// execution trace intact for diagnostics and leave other channels untouched.
function displayQuery(query: string) {
  if (selectedSummary.value?.channel !== 'embed') return query
  return restoreEmbedMessageDisplay({ role: 'user', content: query }).content
}

function formatTokens(value: number) {
  return new Intl.NumberFormat(locale.value).format(value || 0)
}

function formatDate(value: string) {
  if (!value) return '--'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '--'
  return new Intl.DateTimeFormat(locale.value, {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
  }).format(date)
}

function channelLabel(summary: Pick<SessionUsageSummary, 'channel' | 'channel_id'>) {
  const channels: Record<string, string> = {
    web: 'web', api: 'api', embed: 'embed', wecom: 'wecom', wechat: 'wechat',
    feishu: 'feishu', lark: 'lark', dingtalk: 'dingtalk', slack: 'slack',
    telegram: 'telegram', mattermost: 'mattermost', qqbot: 'qqbot', yunzhijia: 'yunzhijia',
  }
  const key = channels[summary.channel] || 'other'
  const label = t(`sessionManagement.channels.${key}`, { channel: summary.channel })
  return summary.channel === 'embed' && summary.channel_id ? `${label} (${summary.channel_id})` : label
}

function usageValue(detail: SessionUsageDetail, key: keyof SessionTokenUsage) {
  if (!detail.has_usage || !detail.usage) return '--'
  return formatTokens(Number(detail.usage[key] || 0))
}

function formatScore(value: number | undefined) {
  return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(4) : '--'
}


onMounted(() => { void loadSessions() })
</script>

<style scoped lang="less">
.session-management { display: flex; flex: 1; min-width: 0; min-height: 0; flex-direction: column; padding: 30px 36px; overflow: auto; }
.session-management__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 24px; }
.session-management__header h1 { margin: 0; color: var(--td-text-color-primary); font-size: 22px; font-weight: 600; letter-spacing: 0; }
.session-management__header p { max-width: 680px; margin: 8px 0 0; color: var(--td-text-color-secondary); font-size: var(--app-text-base); line-height: 1.55; }
.session-management__toolbar { display: flex; width: min(720px, 100%); gap: 10px; margin-bottom: 20px; }
.session-management__toolbar :deep(.t-input) { flex: 1; }
.session-management__toolbar :deep(.t-select) { flex: 0 0 156px; width: 156px; }
.session-management__table-section { min-width: 0; }
.session-management__table-shell { display: flex; flex-direction: column; overflow: hidden; border: 1px solid var(--td-component-stroke); border-radius: var(--app-radius-sm); background: var(--td-bg-color-container); }
.session-management__table-shell > .data-table-shell__scroll { min-width: 0; overflow-x: auto; }
.session-management__table-shell > .data-table-shell__pager { display: flex; align-items: center; justify-content: space-between; gap: 16px; min-height: 52px; padding: 8px 14px; border-top: 1px solid var(--td-component-stroke); color: var(--td-text-color-secondary); font-size: var(--app-text-md); }
.session-management__session-cell, .session-management__agent-cell { display: flex; min-width: 0; flex-direction: column; gap: 3px; }
.session-management__title { overflow: hidden; color: var(--td-text-color-primary); font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.session-management code { overflow: hidden; color: var(--td-text-color-placeholder); font-family: var(--td-font-family); font-size: var(--app-text-sm); text-overflow: ellipsis; white-space: nowrap; }
.session-management__agents { display: flex; flex-wrap: wrap; gap: 4px; }
.session-management__agent { max-width: 138px; overflow: hidden; padding: 2px 6px; border-radius: 3px; background: var(--td-bg-color-secondarycontainer); color: var(--td-text-color-secondary); font-size: var(--app-text-sm); text-overflow: ellipsis; white-space: nowrap; }
.session-management__muted { color: var(--td-text-color-placeholder); font-size: var(--app-text-sm); }
.session-management__state, .session-management__drawer-state { display: flex; align-items: center; gap: 10px; padding: 24px; color: var(--td-text-color-secondary); }
.session-management__state--error, .session-management__drawer-state--error { color: var(--td-error-color); }
.session-management__summary-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); margin-bottom: 28px; border-bottom: 1px solid var(--td-component-stroke); }
.session-management__summary-grid > div { display: flex; min-width: 0; flex-direction: column; gap: 6px; padding: 0 16px 18px 0; }
.session-management__summary-grid span { color: var(--td-text-color-secondary); font-size: var(--app-text-sm); }
.session-management__summary-grid strong { color: var(--td-text-color-primary); font-size: var(--app-text-3xl); font-weight: 600; }
.session-management__detail-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
.session-management__detail-heading h2 { margin: 0; color: var(--td-text-color-primary); font-size: var(--app-text-xl); font-weight: 600; }
.session-management__detail-heading span { color: var(--td-text-color-secondary); font-size: var(--app-text-md); }
.session-management__details-table { overflow-x: auto; border: 1px solid var(--td-component-stroke); border-radius: var(--app-radius-sm); }
.session-management__trace-heading { margin-top: 28px; }
.session-management__empty-trace { padding: 20px; border: 1px dashed var(--td-component-stroke); border-radius: var(--app-radius-sm); color: var(--td-text-color-secondary); font-size: var(--app-text-md); }
.session-management__query { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.session-management__question-preview { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.session-management__question-content { max-height: 280px; overflow: auto; overscroll-behavior: contain; white-space: pre-wrap; overflow-wrap: anywhere; margin: 0 0 24px; padding: 16px; border: 1px solid var(--td-component-stroke); border-radius: var(--app-radius-sm); background: var(--td-bg-color-container); color: var(--td-text-color-primary); font-family: var(--td-font-family); font-size: var(--app-text-base); line-height: 1.75; }
.session-management__trace-detail { margin-top: 24px; min-width: 0; overflow-wrap: anywhere; }
.session-management__trace-outcome { margin-bottom: 20px; }
.session-management__trace-outcome h3 { font-size: var(--app-text-base); margin: 20px 0 10px; }
.session-management__trace-query, .session-management__trace-step-content p { display: flex; flex-wrap: wrap; gap: 8px; margin: 8px 0; color: var(--td-text-color-primary); font-size: var(--app-text-md); line-height: 1.55; }
.session-management__trace-query span, .session-management__trace-step-content p span { color: var(--td-text-color-secondary); }
.session-management__trace-step { margin-top: 10px; border: 1px solid var(--td-component-stroke); border-radius: var(--app-radius-sm); background: var(--td-bg-color-container); }
.session-management__trace-step summary { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 14px; cursor: pointer; }
.session-management__trace-step-content { padding: 0 14px 14px; }
.session-management__trace-error { color: var(--td-error-color); }
@media (max-width: 760px) { .session-management { padding: 22px 18px; } .session-management__toolbar { width: 100%; } .session-management__summary-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); row-gap: 14px; } .session-management__table-shell > .data-table-shell__pager { align-items: flex-end; flex-direction: column; } }
</style>
