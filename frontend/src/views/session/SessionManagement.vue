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
                  {{ agent.name || t('sessionManagement.unknownAgent') }}
                </span>
              </div>
              <span v-else class="session-management__muted">{{ t('sessionManagement.unknownAgent') }}</span>
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
            <template #agent="{ row }">
              <div class="session-management__agent-cell">
                <span>{{ row.agent_name || t('sessionManagement.unknownAgent') }}</span>
              </div>
            </template>
            <template #model="{ row }"><span v-if="row.model_name">{{ row.model_name }}</span><span v-else>--</span></template>
            <template #input="{ row }">{{ usageValue(row, 'prompt_tokens') }}</template>
            <template #output="{ row }">{{ usageValue(row, 'completion_tokens') }}</template>
            <template #cache="{ row }">{{ usageValue(row, 'cache_read_tokens') }}</template>
            <template #total="{ row }"><strong>{{ usageValue(row, 'total_tokens') }}</strong></template>
          </t-table>
        </div>
      </template>
    </SettingDrawer>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import SettingDrawer from '@/components/settings/SettingDrawer.vue'
import {
  getSessionUsage,
  listSessionUsage,
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
  { colKey: 'agent', title: t('sessionManagement.columns.agent'), width: 220, ellipsis: true },
  { colKey: 'model', title: t('sessionManagement.columns.model'), width: 180, ellipsis: true },
  { colKey: 'input', title: t('sessionManagement.columns.input'), width: 104, align: 'right' },
  { colKey: 'output', title: t('sessionManagement.columns.output'), width: 104, align: 'right' },
  { colKey: 'cache', title: t('sessionManagement.columns.cacheRead'), width: 112, align: 'right' },
  { colKey: 'total', title: t('sessionManagement.columns.total'), width: 104, align: 'right' },
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
  selectedSummary.value = summary
  details.value = []
  detailsError.value = ''
  detailsVisible.value = true
  detailsLoading.value = true
  try {
    const response = await getSessionUsage(summary.session_id)
    selectedSummary.value = response.data.summary
    details.value = response.data.details || []
  } catch (err: any) {
    detailsError.value = err?.message || t('sessionManagement.loadDetailsFailed')
  } finally {
    detailsLoading.value = false
  }
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

onMounted(() => { void loadSessions() })
</script>

<style scoped lang="less">
.session-management { display: flex; flex: 1; min-width: 0; min-height: 0; flex-direction: column; padding: 30px 36px; overflow: auto; }
.session-management__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 24px; }
.session-management__header h1 { margin: 0; color: var(--td-text-color-primary); font-size: 22px; font-weight: 600; letter-spacing: 0; }
.session-management__header p { max-width: 680px; margin: 8px 0 0; color: var(--td-text-color-secondary); font-size: 14px; line-height: 1.55; }
.session-management__toolbar { display: flex; width: min(720px, 100%); gap: 10px; margin-bottom: 20px; }
.session-management__toolbar :deep(.t-input) { flex: 1; }
.session-management__toolbar :deep(.t-select) { flex: 0 0 156px; width: 156px; }
.session-management__table-section { min-width: 0; }
.session-management__table-shell { display: flex; flex-direction: column; overflow: hidden; border: 1px solid var(--td-component-stroke); border-radius: 6px; background: var(--td-bg-color-container); }
.session-management__table-shell > .data-table-shell__scroll { min-width: 0; overflow-x: auto; }
.session-management__table-shell > .data-table-shell__pager { display: flex; align-items: center; justify-content: space-between; gap: 16px; min-height: 52px; padding: 8px 14px; border-top: 1px solid var(--td-component-stroke); color: var(--td-text-color-secondary); font-size: 13px; }
.session-management__session-cell, .session-management__agent-cell { display: flex; min-width: 0; flex-direction: column; gap: 3px; }
.session-management__title { overflow: hidden; color: var(--td-text-color-primary); font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.session-management code { overflow: hidden; color: var(--td-text-color-placeholder); font-family: var(--td-font-family); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.session-management__agents { display: flex; flex-wrap: wrap; gap: 4px; }
.session-management__agent { max-width: 138px; overflow: hidden; padding: 2px 6px; border-radius: 3px; background: var(--td-bg-color-secondarycontainer); color: var(--td-text-color-secondary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.session-management__muted { color: var(--td-text-color-placeholder); font-size: 12px; }
.session-management__state, .session-management__drawer-state { display: flex; align-items: center; gap: 10px; padding: 24px; color: var(--td-text-color-secondary); }
.session-management__state--error, .session-management__drawer-state--error { color: var(--td-error-color); }
.session-management__summary-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); margin-bottom: 28px; border-bottom: 1px solid var(--td-component-stroke); }
.session-management__summary-grid > div { display: flex; min-width: 0; flex-direction: column; gap: 6px; padding: 0 16px 18px 0; }
.session-management__summary-grid span { color: var(--td-text-color-secondary); font-size: 12px; }
.session-management__summary-grid strong { color: var(--td-text-color-primary); font-size: 20px; font-weight: 600; }
.session-management__detail-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
.session-management__detail-heading h2 { margin: 0; color: var(--td-text-color-primary); font-size: 16px; font-weight: 600; }
.session-management__detail-heading span { color: var(--td-text-color-secondary); font-size: 13px; }
.session-management__details-table { overflow-x: auto; border: 1px solid var(--td-component-stroke); border-radius: 6px; }
@media (max-width: 760px) { .session-management { padding: 22px 18px; } .session-management__toolbar { width: 100%; } .session-management__summary-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); row-gap: 14px; } .session-management__table-shell > .data-table-shell__pager { align-items: flex-end; flex-direction: column; } }
</style>
