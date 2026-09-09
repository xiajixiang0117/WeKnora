import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'

const component = readFileSync(new URL('./DataSourceSettings.vue', import.meta.url), 'utf8')
const zhCN = readFileSync(new URL('../../../i18n/locales/zh-CN.ts', import.meta.url), 'utf8')
const enUS = readFileSync(new URL('../../../i18n/locales/en-US.ts', import.meta.url), 'utf8')
const koKR = readFileSync(new URL('../../../i18n/locales/ko-KR.ts', import.meta.url), 'utf8')
const ruRU = readFileSync(new URL('../../../i18n/locales/ru-RU.ts', import.meta.url), 'utf8')

test('renders website crawlers as manual sync instead of generic schedule and sync status', () => {
  assert.match(component, /v-if="ds\.type === 'web_crawler'"/)
  assert.match(component, /datasource\.webCrawler\.manualSync/)
})

test('keeps long data source errors inside their card', () => {
  assert.match(component, /&__error\s*\{[\s\S]*?max-width:\s*100%/)
  assert.match(component, /&__error-text\s*\{[\s\S]*?min-width:\s*0/)
  assert.match(component, /&__error-text\s*\{[\s\S]*?overflow-wrap:\s*anywhere/)
  assert.match(component, /&__error-text\s*\{[\s\S]*?-webkit-line-clamp:\s*2/)
})

test('defines the website crawler manual-sync label in every supported locale', () => {
  for (const locale of [zhCN, enUS, koKR, ruRU]) {
    assert.match(locale, /manualSync:/)
  }
})
