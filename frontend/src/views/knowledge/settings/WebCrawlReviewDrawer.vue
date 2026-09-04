<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import {
  createWebCrawlScan,
  listWebCrawlScans,
  listWebCrawlChanges,
  applyWebCrawlChanges,
  retryWebCrawlChanges,
  type WebCrawlScan,
  type WebCrawlChange,
} from '@/api/datasource'

const props = defineProps<{ dataSourceId: string; dataSourceName?: string }>()
const visible = defineModel<boolean>('visible', { default: false })
const { t } = useI18n()
const scans = ref<WebCrawlScan[]>([])
const changes = ref<WebCrawlChange[]>([])
const selected = ref<string[]>([])
const missingActions = ref<Record<string, string>>({})
const loading = ref(false)
const submitting = ref(false)
const activeScan = computed(() => scans.value[0])
const canApplySelected = computed(() => selected.value.length > 0 && ['review_ready', 'partial_failed'].includes(activeScan.value?.status || ''))
let timer: number | null = null

function stopPolling() { if (timer !== null) { window.clearTimeout(timer); timer = null } }
function schedulePolling() {
  stopPolling()
  if (visible.value && activeScan.value && ['scanning', 'applying'].includes(activeScan.value.status)) {
    timer = window.setTimeout(() => load(), 2500)
  }
}
async function load() {
  if (!props.dataSourceId) return
  loading.value = true
  try {
    const scanRes = await listWebCrawlScans(props.dataSourceId)
    scans.value = scanRes?.data || scanRes || []
    if (activeScan.value) {
      const changeRes = await listWebCrawlChanges(activeScan.value.id, { limit: 200 })
      changes.value = changeRes?.data || changeRes || []
      missingActions.value = Object.fromEntries(changes.value.filter(c => c.change_type === 'missing').map(c => [c.id, c.action || 'keep']))
      selected.value = changes.value.filter(c => c.apply_status === 'pending' && c.change_type !== 'failed').map(c => c.id)
    }
    schedulePolling()
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('datasource.webCrawler.loadFailed'))
  } finally { loading.value = false }
}
async function checkUpdates() {
  submitting.value = true
  try {
    await createWebCrawlScan(props.dataSourceId)
    MessagePlugin.success(t('datasource.webCrawler.scanStarted'))
    await load()
  } catch (e: any) { MessagePlugin.error(e?.message || t('datasource.webCrawler.scanFailed')) }
  finally { submitting.value = false }
}
async function applySelected() {
  if (!activeScan.value || selected.value.length === 0) return
  submitting.value = true
  try {
    await applyWebCrawlChanges(activeScan.value.id, selected.value, missingActions.value)
    MessagePlugin.success(t('datasource.webCrawler.applyStarted'))
    await load()
  } catch (e: any) { MessagePlugin.error(e?.message || t('datasource.webCrawler.applyFailed')) }
  finally { submitting.value = false }
}
async function retryFailed() {
  if (!activeScan.value) return
  const ids = changes.value.filter(c => c.apply_status === 'failed').map(c => c.id)
  if (!ids.length) return
  submitting.value = true
  try { await retryWebCrawlChanges(activeScan.value.id, ids); await load() }
  catch (e: any) { MessagePlugin.error(e?.message || t('datasource.webCrawler.applyFailed')) }
  finally { submitting.value = false }
}
function changeLabel(type: string) { return t(`datasource.webCrawler.change.${type}`) }
function statusLabel(status?: string) { return status ? t(`datasource.webCrawler.status.${status}`) : '--' }
function toggle(id: string) { selected.value = selected.value.includes(id) ? selected.value.filter(v => v !== id) : [...selected.value, id] }
watch(visible, v => { if (v) { load() } else stopPolling() })
onBeforeUnmount(stopPolling)
</script>

<template>
  <t-drawer v-model:visible="visible" size="720px" destroy-on-close>
    <template #header>
      <div class="web-crawl-header">
        <span>{{ t('datasource.webCrawler.reviewTitle') }}<template v-if="props.dataSourceName"> · {{ props.dataSourceName }}</template></span>
        <t-button size="small" theme="primary" :loading="submitting" @click="checkUpdates">{{ t('datasource.webCrawler.checkUpdates') }}</t-button>
      </div>
    </template>
    <div v-if="loading && !activeScan" class="web-crawl-loading"><t-loading /></div>
    <template v-else-if="activeScan">
      <div class="web-crawl-summary">
        <t-tag theme="primary" variant="light">{{ statusLabel(activeScan.status) }}</t-tag>
        <span>{{ t('datasource.webCrawler.summary', { total: activeScan.items_total, added: activeScan.items_added, updated: activeScan.items_updated, missing: activeScan.items_missing, failed: activeScan.items_failed }) }}</span>
      </div>
      <div v-if="changes.length === 0" class="web-crawl-empty">{{ t('datasource.webCrawler.noChanges') }}</div>
      <div v-else class="web-crawl-changes">
        <div v-for="change in changes" :key="change.id" class="web-crawl-change">
          <input v-if="change.change_type !== 'failed' && change.apply_status === 'pending'" type="checkbox" :checked="selected.includes(change.id)" @change="toggle(change.id)">
          <span class="change-type" :class="`change-type--${change.change_type}`">{{ changeLabel(change.change_type) }}</span>
          <div class="change-main"><strong :title="change.canonical_url">{{ change.title || change.canonical_url }}</strong><small>{{ change.canonical_url }}</small><span>{{ change.summary }}</span><t-select v-if="change.change_type === 'missing'" v-model="missingActions[change.id]" size="small" class="missing-action"><t-option value="keep" :label="t('datasource.webCrawler.change.missing')" /><t-option value="disable" :label="t('datasource.pause')" /><t-option value="delete" :label="t('datasource.delete')" /></t-select><details v-if="change.previous_content || change.new_content" class="change-diff"><summary>{{ t('datasource.logs') }}</summary><div class="diff-columns"><pre>{{ change.previous_content || '∅' }}</pre><pre>{{ change.new_content || '∅' }}</pre></div></details></div>
          <t-tag v-if="change.apply_status !== 'pending'" size="small" variant="light">{{ change.apply_status }}</t-tag>
        </div>
      </div>
    </template>
    <div v-else class="web-crawl-empty">{{ t('datasource.webCrawler.noScan') }}</div>
    <template #footer>
      <t-button variant="outline" :disabled="!changes.some(c => c.apply_status === 'failed')" :loading="submitting" @click="retryFailed">{{ t('datasource.webCrawler.retryFailed') }}</t-button>
      <t-button theme="primary" :disabled="!canApplySelected" :loading="submitting" @click="applySelected">{{ t('datasource.webCrawler.applySelected', { count: selected.length }) }}</t-button>
    </template>
  </t-drawer>
</template>

<style scoped lang="less">
.web-crawl-header { display:flex; align-items:center; justify-content:space-between; gap:12px; width:100%; }
.web-crawl-loading { display:flex; justify-content:center; padding:80px 0; }
.web-crawl-summary { display:flex; align-items:center; gap:10px; padding:12px 14px; margin-bottom:12px; background:var(--td-bg-color-container-hover); border-radius:8px; font-size:13px; color:var(--td-text-color-secondary); }
.web-crawl-empty { padding:70px 0; text-align:center; color:var(--td-text-color-placeholder); }
.web-crawl-change { display:flex; align-items:flex-start; gap:10px; padding:12px 4px; border-bottom:1px solid var(--td-component-stroke); }
.web-crawl-change input { margin-top:4px; }
.change-type { flex:none; min-width:44px; font-size:12px; font-weight:600; }
.change-type--added { color:var(--td-success-color); }.change-type--updated { color:var(--td-brand-color); }.change-type--missing,.change-type--failed { color:var(--td-error-color); }
.change-main { flex:1; min-width:0; display:flex; flex-direction:column; gap:3px; }.change-main strong,.change-main small { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }.change-main small { color:var(--td-text-color-placeholder); }.change-main span { color:var(--td-text-color-secondary); font-size:12px; }
</style>
