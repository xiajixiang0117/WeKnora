import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'
import * as vue from 'vue'

// Execute the real composable with fake transport/browser boundaries.
const source = readFileSync(new URL('./useEmbedBridge.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source.replaceAll('import.meta.env.DEV', 'false'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

async function setup(stored, valid = true, initialToken = 'ems_test') {
  const storage = new Map(stored ? [['chat', JSON.stringify(stored)]] : [])
  let creates = 0
  let ready = 0
  let fail = false
  let finishCreate
  let hostToken
  let unmount
  let now = 0
  let exchanges = 0
  let exchangeFails = false
  let finishExchange
  let holdExchange = false
  const timers = new Map()
  let timerId = 0
  const usedTokens = []
  const browser = {
    localStorage: {
      getItem: key => storage.get(key),
      setItem: (key, value) => storage.set(key, value),
      removeItem: key => storage.delete(key),
    },
  }
  browser.parent = browser
  globalThis.window = browser
  const api = {
    embedChatSessionStorageKey: () => 'chat',
    getOrCreateEmbedVisitorId: () => 'visitor',
    isEmbedSessionToken: token => token.startsWith('ems_'),
    exchangeEmbedSession: async (_channel, token) => {
      assert.equal(token, 'publish')
      exchanges++
      if (exchangeFails) throw new Error('exchange offline')
      if (holdExchange) await new Promise(resolve => { finishExchange = resolve })
      return { data: { session_token: `ems_${exchanges}`, expires_in: 1800 } }
    },
    getEmbedConfig: async () => ({ success: true, data: { agent_id: 'agent' } }),
    getEmbedMessageList: async () => { if (!valid) throw new Error('stale') },
    createEmbedSession: async (_channel, token) => {
      usedTokens.push(token)
      creates++
      if (fail) throw new Error('offline')
      await new Promise(resolve => { finishCreate = resolve })
      return { data: { id: 'created', sig: 'signature' } }
    },
    postEmbedReady: () => ready++,
    onEmbedHostContext: () => () => {},
    onEmbedHostLocale: () => () => {},
    onEmbedHostToken: handler => { hostToken = handler; return () => {} },
  }
  let mounted
  const exports = {}
  new Function('require', 'exports', 'setTimeout', 'clearTimeout', 'Date', compiled)(name => {
    if (name === 'vue') return { ...vue, onMounted: fn => { mounted = fn }, onUnmounted: fn => { unmount = fn } }
    if (name === 'vue-router') return { useRoute: () => ({ query: { token: initialToken } }) }
    if (name === 'vue-i18n') return { useI18n: () => ({ locale: vue.ref('en'), t: key => key }) }
    if (name === '@/api/embed') return api
    if (name === '@/i18n/embed') return { readEmbedLocaleFromUrl: () => '' }
    throw new Error(`Unexpected import: ${name}`)
  }, exports, (fn, delay) => { timers.set(++timerId, { fn, delay }); return timerId },
  id => timers.delete(id), { now: () => now })
  const bridge = exports.useEmbedBridge(vue.ref('channel'))
  mounted()
  for (let i = 0; i < 10; i++) await Promise.resolve()
  return { bridge, storage, creates: () => creates, ready: () => ready,
    hostToken, unmount, timers, usedTokens, exchanges: () => exchanges,
    advance: ms => { now += ms },
    failExchange: value => { exchangeFails = value },
    holdExchange: () => { holdExchange = true },
    finishExchange: () => finishExchange(),
    fail: value => { fail = value }, finish: () => finishCreate() }
}

async function flush() {
  for (let i = 0; i < 15; i++) await Promise.resolve()
}

test('host token renewal preserves history and local new-chat drafts', async () => {
  const h = await setup({ id: 'history', sig: 'sig', agentId: 'agent' })
  h.hostToken('ems_renewed', 'other-channel')
  assert.equal(h.bridge.token.value, 'ems_test')
  h.hostToken('ems_renewed', 'channel')
  assert.equal(h.bridge.token.value, 'ems_renewed')
  assert.equal(h.bridge.sessionId.value, 'history')
  assert.equal(h.bridge.sessionSig.value, 'sig')
  h.bridge.startNewSession()
  h.hostToken('ems_newer', 'channel')
  assert.equal(h.bridge.sessionId.value, '')
  assert.equal(h.storage.has('chat'), false)
  const send = h.bridge.ensureSession()
  assert.deepEqual(h.usedTokens, ['ems_newer'])
  h.finish()
  await send
  assert.equal(h.exchanges(), 0)
})

test('publish mode renews before expiry without resetting the chat', async () => {
  const h = await setup({ id: 'history', sig: 'sig', agentId: 'agent' }, true, 'publish')
  assert.equal(h.bridge.token.value, 'ems_1')
  const timer = [...h.timers.values()][0]
  assert.equal(timer.delay, 24 * 60_000)
  timer.fn()
  await flush()
  assert.equal(h.bridge.token.value, 'ems_2')
  assert.equal(h.bridge.sessionId.value, 'history')
  assert.equal(h.bridge.sessionSig.value, 'sig')
  h.unmount()
  assert.equal(h.timers.size, 0)
})

test('throttled timers are compensated on send and concurrent sends share renewal', async () => {
  const h = await setup(undefined, true, 'publish')
  h.advance(31 * 60_000)
  h.holdExchange()
  const first = h.bridge.ensureSession()
  const second = h.bridge.ensureSession()
  assert.equal(h.exchanges(), 2)
  assert.equal(h.creates(), 0)
  h.finishExchange()
  await flush()
  assert.equal(h.creates(), 1)
  assert.deepEqual(h.usedTokens, ['ems_2'])
  h.finish()
  await Promise.all([first, second])
  h.unmount()
})

test('renewal failure retains the old token and schedules a retry', async () => {
  const h = await setup(undefined, true, 'publish')
  h.failExchange(true)
  ;[...h.timers.values()][0].fn()
  await flush()
  assert.equal(h.bridge.token.value, 'ems_1')
  assert.equal([...h.timers.values()][0].delay, 30_000)
  h.failExchange(false)
  ;[...h.timers.values()][0].fn()
  await flush()
  assert.equal(h.bridge.token.value, 'ems_3')
  h.unmount()
})

test('unmount ignores an in-flight renewal response', async () => {
  const h = await setup(undefined, true, 'publish')
  h.holdExchange()
  ;[...h.timers.values()][0].fn()
  h.unmount()
  h.finishExchange()
  await flush()
  assert.equal(h.bridge.token.value, 'ems_1')
  assert.equal(h.timers.size, 0)
})

test('opening and new-chat stay local; first send creates once even with concurrent callers', async () => {
  const h = await setup()
  assert.equal(h.ready(), 1)
  assert.equal(h.bridge.loadError.value, '')
  assert.equal(h.bridge.sessionId.value, '')
  h.bridge.startNewSession()
  assert.equal(h.creates(), 0)
  const first = h.bridge.ensureSession()
  const second = h.bridge.ensureSession()
  assert.equal(h.creates(), 1)
  h.finish()
  await Promise.all([first, second])
  assert.equal(h.bridge.sessionId.value, 'created')
  assert.equal(h.bridge.sessionSig.value, 'signature')
  assert.equal(JSON.parse(h.storage.get('chat')).id, 'created')
  await h.bridge.ensureSession()
  assert.equal(h.creates(), 1)
  h.bridge.startNewSession()
  assert.equal(h.bridge.sessionId.value, '')
  assert.equal(h.storage.has('chat'), false)
  assert.equal(h.creates(), 1)
})

test('valid history resumes without creating; stale or rebound history becomes a local draft', async () => {
  const stored = { id: 'history', sig: 'sig', agentId: 'agent' }
  for (const [entry, valid, expected] of [[stored, true, 'history'], [stored, false, ''], [{ ...stored, agentId: 'old' }, true, '']]) {
    const h = await setup(entry, valid)
    assert.equal(h.bridge.sessionId.value, expected)
    assert.equal(h.creates(), 0)
    assert.equal(h.ready(), 1)
  }
})

test('creation failure can be retried without breaking the page', async () => {
  const h = await setup()
  h.fail(true)
  await assert.rejects(h.bridge.ensureSession(), /offline/)
  assert.equal(h.bridge.sessionId.value, '')
  assert.equal(h.bridge.loadError.value, '')
  h.fail(false)
  const retry = h.bridge.ensureSession()
  h.finish()
  await retry
  assert.equal(h.creates(), 2)
  assert.equal(h.bridge.sessionId.value, 'created')
})
