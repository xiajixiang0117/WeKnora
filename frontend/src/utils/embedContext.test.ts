import assert from 'node:assert/strict'
import test from 'node:test'
import { buildQueryWithHostContext, restoreEmbedMessageDisplay } from './embedContext.ts'

test('restored embed history displays the original question and preserves server content', () => {
  const query = '介绍一下HAL_PMU_SelectWakeupPin'
  const content = buildQueryWithHostContext(query, {
    page_url: 'http://127.0.0.1:8000/projects/sdk/latest/sf32lb52x/index.html',
    page_title: 'SiFli SDK编程指南 文档',
    page_chip: 'SF32LB52X',
  })
  const message = { id: 'user-1', role: 'user', content, images: [{ url: '/image.png' }] }
  assert.deepEqual(restoreEmbedMessageDisplay(message), { ...message, content: query })
  assert.equal(message.content, content)
})

test('preserves multiline questions and removes only one generated prefix', () => {
  const query = '第一行\n\n[Host context]\nexample: literal\n\n最后一行'
  const content = buildQueryWithHostContext(query, { chip: 'SF32LB52X', options: { enabled: true } })
  assert.equal(restoreEmbedMessageDisplay({ role: 'user', content }).content, query)
})

test('leaves plain questions, malformed prefixes, and assistant messages intact', () => {
  for (const content of [
    '介绍一下HAL_PMU_SelectWakeupPin',
    '解释下面的内容\n[Host context]\npage_chip: SF32LB52X\n\n问题',
    '[Host context]\n普通文本\n\n问题',
    '[Host context]\npage_chip: SF32LB52X',
    '',
    undefined,
  ]) {
    const message = { role: 'user', content }
    assert.equal(restoreEmbedMessageDisplay(message), message)
  }
  const assistant = { role: 'assistant', content: buildQueryWithHostContext('回答', { chip: 'SF32LB52X' }) }
  assert.equal(restoreEmbedMessageDisplay(assistant), assistant)
})

test('empty host context keeps the outbound question unchanged', () => {
  for (const context of [undefined, {}, { empty: '', missing: undefined, nil: null }]) {
    assert.equal(buildQueryWithHostContext('问题', context), '问题')
  }
})
