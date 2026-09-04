<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
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
const activeFilter = ref<'all' | 'added' | 'updated' | 'missing' | 'failed'>('all')
const loading = ref(false)
const submitting = ref(false)
const activeScan = computed(() => scans.value[0])
const isScanning = computed(() => activeScan.value?.status === 'scanning')
const visibleChanges = computed(() => activeFilter.value === 'all' ? changes.value : changes.value.filter(change => change.change_type === activeFilter.value))
const selectedVisibleIDs = computed(() => selected.value.filter(id => visibleChanges.value.some(change => change.id === id)))
const selectableVisibleChanges = computed(() => visibleChanges.value.filter(change => change.change_type !== 'failed' && change.apply_status === 'pending'))
const allVisibleSelected = computed(() => selectableVisibleChanges.value.length > 0 && selectableVisibleChanges.value.every(change => selected.value.includes(change.id)))
const canSelectChanges = computed(() => selectableVisibleChanges.value.length > 0 && ['review_ready', 'partial_failed'].includes(activeScan.value?.status || ''))
const canApplySelected = computed(() => selectedVisibleIDs.value.length > 0 && ['review_ready', 'partial_failed'].includes(activeScan.value?.status || ''))
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
      const pageSize = 200
      const allChanges: WebCrawlChange[] = []
      for (let offset = 0; ; offset += pageSize) {
        const changeRes = await listWebCrawlChanges(activeScan.value.id, { limit: pageSize, offset })
        const page = changeRes?.data || changeRes || []
        allChanges.push(...page)
        if (page.length < pageSize) break
      }
      changes.value = allChanges
      missingActions.value = Object.fromEntries(changes.value.filter(c => c.change_type === 'missing').map(c => [c.id, c.action || 'keep']))
      selected.value = []
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
  const ids = [...selectedVisibleIDs.value]
  if (!activeScan.value || ids.length === 0 || !canApplySelected.value) return
  const actions = { ...missingActions.value }
  const dialog = DialogPlugin.confirm({
    header: t('common.confirm'),
    body: t('datasource.webCrawler.applyConfirm', { count: ids.length }),
    confirmBtn: t('common.confirm'),
    cancelBtn: t('common.cancel'),
    onConfirm: async () => {
      dialog.destroy()
      await submitSelected(ids, actions)
    },
    onCancel: () => dialog.destroy(),
  })
}
async function submitSelected(ids: string[], actions: Record<string, string>) {
  if (!activeScan.value) return
  submitting.value = true
  try {
    await applyWebCrawlChanges(activeScan.value.id, ids, actions)
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
function toggleSelectAll() {
  const ids = selectableVisibleChanges.value.map(change => change.id)
  if (allVisibleSelected.value) {
    selected.value = selected.value.filter(id => !ids.includes(id))
  } else {
    selected.value = Array.from(new Set([...selected.value, ...ids]))
  }
}
function setFilter(filter: typeof activeFilter.value) { activeFilter.value = activeFilter.value === filter && filter !== 'all' ? 'all' : filter }
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
        <div class="web-crawl-filters">
          <button type="button" :class="{ active: activeFilter === 'all' }" @click="setFilter('all')">{{ t('datasource.webCrawler.summary', { total: activeScan.items_total, added: activeScan.items_added, updated: activeScan.items_updated, missing: activeScan.items_missing, failed: activeScan.items_failed }) }}</button>
          <button type="button" :class="{ active: activeFilter === 'added' }" @click="setFilter('added')">{{ t('datasource.webCrawler.change.added') }} {{ activeScan.items_added }}</button>
          <button type="button" :class="{ active: activeFilter === 'updated' }" @click="setFilter('updated')">{{ t('datasource.webCrawler.change.updated') }} {{ activeScan.items_updated }}</button>
          <button type="button" :class="{ active: activeFilter === 'missing' }" @click="setFilter('missing')">{{ t('datasource.webCrawler.change.missing') }} {{ activeScan.items_missing }}</button>
          <button type="button" :class="{ active: activeFilter === 'failed' }" @click="setFilter('failed')">{{ t('datasource.webCrawler.change.failed') }} {{ activeScan.items_failed }}</button>
        </div>
      </div>
      <div v-if="isScanning" class="web-crawl-loading"><t-loading size="36px" /></div>
      <div v-else-if="visibleChanges.length === 0" class="web-crawl-empty">{{ t('datasource.webCrawler.noChanges') }}</div>
      <div v-else class="web-crawl-changes">
        <div v-for="change in visibleChanges" :key="change.id" class="web-crawl-change">
          <input v-if="change.change_type !== 'failed' && change.apply_status === 'pending'" type="checkbox" :checked="selected.includes(change.id)" @change="toggle(change.id)">
          <span class="change-type" :class="`change-type--${change.change_type}`">{{ changeLabel(change.change_type) }}</span>
          <div class="change-main"><strong :title="change.canonical_url">{{ change.title || change.canonical_url }}</strong><small>{{ change.canonical_url }}</small><span>{{ change.summary }}</span><t-select v-if="change.change_type === 'missing'" v-model="missingActions[change.id]" size="small" class="missing-action"><t-option value="keep" :label="t('datasource.webCrawler.change.missing')" /><t-option value="disable" :label="t('datasource.pause')" /><t-option value="delete" :label="t('datasource.delete')" /></t-select><details v-if="change.previous_content || change.new_content" class="change-diff"><summary>{{ t('datasource.logs') }}</summary><div class="diff-columns"><pre>{{ change.previous_content || '∅' }}</pre><pre>{{ change.new_content || '∅' }}</pre></div></details></div>
          <t-tag v-if="change.apply_status !== 'pending'" size="small" variant="light">{{ change.apply_status }}</t-tag>
        </div>
      </div>
    </template>
    <div v-else class="web-crawl-empty">{{ t('datasource.webCrawler.noScan') }}</div>
    <template #footer>
      <div class="web-crawl-footer">
        <t-button variant="outline" :disabled="!canSelectChanges" @click="toggleSelectAll">{{ t(allVisibleSelected ? 'datasource.webCrawler.clearSelection' : 'datasource.webCrawler.selectAll') }}</t-button>
        <div class="web-crawl-footer-actions">
          <t-button variant="outline" :disabled="!changes.some(c => c.apply_status === 'failed')" :loading="submitting" @click="retryFailed">{{ t('datasource.webCrawler.retryFailed') }}</t-button>
          <t-button theme="primary" :disabled="!canApplySelected" :loading="submitting" @click="applySelected">{{ t('datasource.webCrawler.applySelected', { count: selectedVisibleIDs.length }) }}</t-button>
        </div>
      </div>
    </template>
  </t-drawer>
</template>

<style scoped lang="less">
.web-crawl-header { display:flex; align-items:center; justify-content:space-between; gap:12px; width:100%; }
.web-crawl-footer { display:flex; align-items:center; justify-content:space-between; gap:12px; width:100%; }
.web-crawl-footer-actions { display:flex; align-items:center; gap:8px; }
.web-crawl-loading { display:flex; justify-content:center; padding:80px 0; }
.web-crawl-summary { display:flex; align-items:center; gap:10px; padding:12px 14px; margin-bottom:12px; background:var(--td-bg-color-container-hover); border-radius:8px; font-size:13px; color:var(--td-text-color-secondary); }
.web-crawl-filters { display:flex; align-items:center; flex-wrap:wrap; gap:4px; }
.web-crawl-filters button { border:0; padding:3px 6px; background:transparent; color:inherit; cursor:pointer; font:inherit; border-radius:4px; }
.web-crawl-filters button:hover,.web-crawl-filters button.active { background:var(--td-bg-color-container); color:var(--td-brand-color); }
.web-crawl-empty { padding:70px 0; text-align:center; color:var(--td-text-color-placeholder); }
.web-crawl-change { display:flex; align-items:flex-start; gap:10px; padding:12px 4px; border-bottom:1px solid var(--td-component-stroke); }
.web-crawl-change input { margin-top:4px; }
.change-type { flex:none; min-width:44px; font-size:12px; font-weight:600; }
.change-type--added { color:var(--td-success-color); }.change-type--updated { color:var(--td-brand-color); }.change-type--missing,.change-type--failed { color:var(--td-error-color); }
.change-main { flex:1; min-width:0; display:flex; flex-direction:column; gap:3px; }.change-main strong,.change-main small { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }.change-main small { color:var(--td-text-color-placeholder); }.change-main span { color:var(--td-text-color-secondary); font-size:12px; }
</style>
