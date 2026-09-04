import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'

const source = readFileSync(new URL('./DataSourceEditorDialog.vue', import.meta.url), 'utf8')

test('website crawler skips the empty credential step and configures scope in its resource step', () => {
  assert.match(source, /function hasCredentialStep\(\)[\s\S]*?!isWebCrawlerConnector\(form\.value\.type\)/)
  assert.match(source, /v-if="step === 2 && isWebCrawlerConnector\(form\.type\)"/)
  assert.match(source, /<section v-if="hasCredentialStep\(\)" class="setting-drawer__section">/)
	assert.match(source, /v-model="form\.config\.settings\.web_content_selector"/)
	assert.match(source, /v-model="form\.config\.settings\.web_exclude_selectors"/)
	assert.match(source, /settings\.web_content_selector = String\(settings\.web_content_selector \|\| ''\)\.trim\(\)/)
})
