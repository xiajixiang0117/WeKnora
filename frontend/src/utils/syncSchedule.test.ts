import assert from 'node:assert/strict'
import test from 'node:test'
import { buildSchedule, describeSchedule, nextScheduleTimes, readSchedule } from './syncSchedule'

test('visual schedules round-trip and preserve existing six-field schedules', () => {
  for (const expression of ['*/15 * * * *', '0 */6 * * *', '0 9 * * *', '30 9 * * 1,3', '0 9 15 * *']) {
    assert.equal(buildSchedule(readSchedule(expression)), expression)
  }
  assert.equal(buildSchedule(readSchedule('0 0 */6 * * *')), '0 */6 * * *')
  assert.equal(readSchedule('0 0 */6 * * *').mode, 'hourly')
  assert.equal(buildSchedule(readSchedule('')), '')
  assert.equal(readSchedule('15 0 9 * * MON-FRI').mode, 'advanced')
})

test('weekly Chinese descriptions and previews use explicit time zone', () => {
  const spec = 'CRON_TZ=Asia/Shanghai 30 9 * * 1,3'
  const description = describeSchedule(spec, 'zh-CN')
  assert.match(description, /09:30/)
  assert.match(description, /星期一/)
  assert.match(description, /星期三/)
  assert.deepEqual(nextScheduleTimes(spec, 'UTC', new Date('2026-09-22T00:00:00Z')), [
    '2026-09-23 09:30:00', '2026-09-28 09:30:00', '2026-09-30 09:30:00',
  ])
})

test('server timezone previews, named weekdays and invalid values', () => {
  assert.equal(nextScheduleTimes('0 9 * * WED', 'UTC+08:00', new Date('2026-09-22T00:00:00Z'))[0], '2026-09-23 09:00:00')
  for (const invalid of ['', '60 * * * *', '0 25 * * *', '0 9 31 2 *', '0 9 * *', '0 9 * * MON#1']) {
    assert.throws(() => nextScheduleTimes(invalid, 'UTC'), invalid)
  }
})
