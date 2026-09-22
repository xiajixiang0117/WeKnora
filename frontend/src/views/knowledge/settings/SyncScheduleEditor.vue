<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { buildSchedule, describeSchedule, nextScheduleTimes, readSchedule, type ScheduleMode } from '@/utils/syncSchedule'

const props = defineProps<{ modelValue: string; serverTimezone?: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string]; valid: [value: boolean] }>()
const { t, locale } = useI18n()
const fields = reactive(readSchedule(props.modelValue))
watch(() => props.modelValue, value => {
  if (value !== buildSchedule(fields)) Object.assign(fields, readSchedule(value))
})
const modes: ScheduleMode[] = ['manual', 'minutes', 'hourly', 'daily', 'weekly', 'monthly', 'advanced']
const localTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai'
const timezones = [...new Set([localTimezone, 'Asia/Shanghai', 'UTC', 'Asia/Tokyo', 'America/New_York', 'Europe/London', fields.timezone].filter(Boolean))]
const preview = computed(() => {
  if (!props.modelValue) return { description: '', times: [], error: false }
  try {
    const times = nextScheduleTimes(props.modelValue, props.serverTimezone || 'UTC')
    return { description: describeSchedule(props.modelValue, locale.value), times, error: false }
  } catch { return { description: '', times: [], error: true } }
})
watch(() => preview.value.error, invalid => emit('valid', !invalid), { immediate: true })
function update() {
  if (fields.mode === 'advanced' && /^(?:CRON_TZ|TZ)=/.test(fields.expression.trim())) {
    const parsed = readSchedule(fields.expression)
    fields.timezone = parsed.timezone
    fields.expression = parsed.expression
  }
  emit('update:modelValue', buildSchedule(fields))
}
function changeMinute(value: number) {
  fields.time = `09:${String(value).padStart(2, '0')}`
  update()
}
function changeMode() {
  fields.interval = fields.mode === 'minutes' ? 15 : 1
  if (!fields.timezone && !props.serverTimezone) fields.timezone = localTimezone
  if (fields.mode === 'advanced') fields.expression = readSchedule(props.modelValue).expression || '0 9 * * *'
  update()
}
</script>

<template>
  <div class="sync-schedule-editor">
    <t-select v-model="fields.mode" :aria-label="t('datasource.syncScheduleLabel')" @change="changeMode">
      <t-option v-for="mode in modes" :key="mode" :value="mode" :label="t(`datasource.scheduleEditor.${mode}`)" />
    </t-select>
    <template v-if="fields.mode !== 'manual'">
      <div v-if="fields.mode === 'minutes' || fields.mode === 'hourly'" class="schedule-row">
        <label>{{ t('datasource.scheduleEditor.interval') }}</label>
        <t-input-number v-model="fields.interval" :min="1" :max="fields.mode === 'minutes' ? 59 : 23" @change="update" />
        <span>{{ t(`datasource.scheduleEditor.${fields.mode === 'minutes' ? 'minuteUnit' : 'hourUnit'}`) }}</span>
      </div>
      <div v-if="fields.mode === 'weekly'" class="schedule-weekdays">
        <t-checkbox-group v-model="fields.weekdays" @change="update">
          <t-checkbox v-for="day in [1, 2, 3, 4, 5, 6, 0]" :key="day" :value="day">{{ t(`datasource.scheduleEditor.weekday${day}`) }}</t-checkbox>
        </t-checkbox-group>
      </div>
      <div v-if="fields.mode === 'monthly'" class="schedule-row">
        <label>{{ t('datasource.scheduleEditor.monthDay') }}</label>
        <t-input-number v-model="fields.day" :min="1" :max="31" @change="update" />
      </div>
      <div v-if="['daily', 'weekly', 'monthly'].includes(fields.mode)" class="schedule-row">
        <label for="sync-schedule-time">{{ t('datasource.scheduleEditor.atTime') }}</label>
        <input id="sync-schedule-time" v-model="fields.time" type="time" required @change="update" />
      </div>
      <div v-if="fields.mode === 'hourly'" class="schedule-row">
        <label>{{ t('datasource.scheduleEditor.atMinute') }}</label>
        <t-input-number :model-value="Number(fields.time.split(':')[1])" :min="0" :max="59" @change="changeMinute" />
      </div>
      <div v-if="fields.mode === 'advanced'" class="schedule-field">
        <label>{{ t('datasource.cronExpression') }}</label>
        <t-input v-model="fields.expression" placeholder="0 9 * * 1-5" @change="update" />
      </div>
      <div class="schedule-field">
        <label>{{ t('datasource.scheduleEditor.timezone') }}</label>
        <t-select v-model="fields.timezone" :aria-label="t('datasource.scheduleEditor.timezone')" @change="update">
          <t-option v-if="props.serverTimezone || !fields.timezone" value="" :label="`${t('datasource.scheduleEditor.serverTime')} ${props.serverTimezone || ''}`" />
          <t-option v-for="zone in timezones" :key="zone" :value="zone" :label="zone" />
        </t-select>
      </div>
      <p v-if="preview.error" class="schedule-error" role="alert">{{ t('datasource.scheduleEditor.invalid') }}</p>
      <div v-else class="schedule-preview" aria-live="polite">
        <strong>{{ preview.description }}</strong>
        <template v-if="fields.timezone || props.serverTimezone">
          <span>{{ t('datasource.scheduleEditor.nextRuns') }}</span>
          <ol><li v-for="time in preview.times" :key="time">{{ time }}</li></ol>
        </template>
      </div>
    </template>
  </div>
</template>

<style scoped>
.sync-schedule-editor { display: grid; gap: 16px; min-width: 0; }
.schedule-row { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; }
.schedule-field { display: grid; gap: 8px; }
.schedule-row label, .schedule-field label { font-size: 13px; color: var(--td-text-color-secondary); }
.schedule-weekdays :deep(.t-checkbox-group) { display: flex; flex-wrap: wrap; gap: 12px; }
.schedule-weekdays :deep(.t-checkbox) { margin-right: 0; }
.schedule-row input[type="time"] { border: 1px solid var(--td-border-level-2-color); border-radius: 4px; padding: 6px 10px; color: var(--td-text-color-primary); background: var(--td-bg-color-container); font: inherit; }
.schedule-preview { display: grid; gap: 8px; font-size: 13px; overflow-wrap: anywhere; }
.schedule-preview strong { font-weight: 500; }
.schedule-preview span, .schedule-preview ol { color: var(--td-text-color-secondary); }
.schedule-preview ol { margin: 0; padding-left: 22px; line-height: 1.8; font-variant-numeric: tabular-nums; }
.schedule-error { margin: 0; font-size: 13px; color: var(--td-error-color); }
</style>
