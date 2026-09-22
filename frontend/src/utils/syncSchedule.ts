import { CronExpressionParser } from 'cron-parser'
import cronstrue from 'cronstrue/i18n'

export type ScheduleMode = 'manual' | 'minutes' | 'hourly' | 'daily' | 'weekly' | 'monthly' | 'advanced'
export interface ScheduleFields {
  mode: ScheduleMode
  interval: number
  time: string
  weekdays: number[]
  day: number
  timezone: string
  expression: string
}

export function readSchedule(value: string): ScheduleFields {
  const match = value.trim().match(/^(?:CRON_TZ|TZ)=(\S+)\s+(.+)$/)
  const expression = match?.[2] ?? value.trim()
  const fields: ScheduleFields = { mode: value ? 'advanced' : 'manual', interval: 1, time: '09:00', weekdays: [1], day: 1, timezone: match?.[1] ?? '', expression }
  let parts = expression.split(/\s+/)
  if (parts.length === 6 && parts[0] === '0') parts = parts.slice(1)
  if (parts.length !== 5) return fields
  const [minute, hour, day, month, week] = parts
  if (month !== '*') return fields
  if (/^\*\/\d+$/.test(minute) && hour === '*' && day === '*' && week === '*') {
    return { ...fields, mode: 'minutes', interval: Number(minute.slice(2)) }
  }
  if (!/^\d+$/.test(minute)) return fields
  if ((hour === '*' || /^\*\/\d+$/.test(hour)) && day === '*' && week === '*') {
    return { ...fields, mode: 'hourly', interval: hour === '*' ? 1 : Number(hour.slice(2)), time: `09:${minute.padStart(2, '0')}` }
  }
  if (!/^\d+$/.test(hour)) return fields
  fields.time = `${hour.padStart(2, '0')}:${minute.padStart(2, '0')}`
  if (day === '*' && week === '*') fields.mode = 'daily'
  else if (day === '*' && /^[0-6](,[0-6])*$/.test(week)) {
    fields.mode = 'weekly'
    fields.weekdays = week.split(',').map(Number)
  } else if (/^\d+$/.test(day) && week === '*') {
    fields.mode = 'monthly'
    fields.day = Number(day)
  }
  return fields
}

export function buildSchedule(fields: ScheduleFields): string {
  if (fields.mode === 'manual') return ''
  const [hour, minute] = fields.time.split(':').map(Number)
  const specs: Record<Exclude<ScheduleMode, 'manual'>, string> = {
    minutes: `*/${fields.interval} * * * *`,
    hourly: `${minute} */${fields.interval} * * *`,
    daily: `${minute} ${hour} * * *`,
    weekly: `${minute} ${hour} * * ${[...fields.weekdays].sort().join(',')}`,
    monthly: `${minute} ${hour} ${fields.day} * *`,
    advanced: fields.expression.trim(),
  }
  const spec = specs[fields.mode]
  return fields.timezone ? `CRON_TZ=${fields.timezone} ${spec}` : spec
}

export function describeSchedule(value: string, locale: string): string {
  const fields = readSchedule(value)
  return cronstrue.toString(fields.expression, { locale: locale.startsWith('zh') ? 'zh_CN' : locale.split('-')[0], use24HourTimeFormat: true, throwExceptionOnParseError: true })
}

export function nextScheduleTimes(value: string, defaultTimezone: string, now = new Date()): string[] {
  const fields = readSchedule(value)
  const expression = fields.expression
  if (!expression.startsWith('@') && ![5, 6].includes(expression.split(/\s+/).length)) throw new Error('Expected five or six cron fields')
  // Match robfig/cron's supported grammar; cron-parser also accepts Quartz extensions.
  if (!expression || /[?#]/.test(expression) || /(?:^|[\s,])(?:L|\d+[LW]|L-\d+)(?=$|[\s,])/.test(expression) || expression.startsWith('@every')) throw new Error('Unsupported cron expression')
  const cron = CronExpressionParser.parse(expression, { currentDate: now, tz: fields.timezone || defaultTimezone })
  const pad = (n: number) => String(n).padStart(2, '0')
  return Array.from({ length: 3 }, () => {
    const date = cron.next()
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
  })
}
