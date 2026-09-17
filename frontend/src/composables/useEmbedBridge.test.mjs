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

async function setup(stored, valid = true) {
  const storage = new Map(stored ? [['chat', JSON.stringify(stored)]] : [])
  let creates = 0
  let ready = 0
  let fail = false
  let finishCreate
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
    isEmbedSessionToken: () => true,
    getEmbedConfig: async () => ({ success: true, data: { agent_id: 'agent' } }),
    getEmbedMessageList: async () => { if (!valid) throw new Error('stale') },
    createEmbedSession: async () => {
      creates++
      if (fail) throw new Error('offline')
      await new Promise(resolve => { finishCreate = resolve })
      return { data: { id: 'created', sig: 'signature' } }
    },
    postEmbedReady: () => ready++,
    onEmbedHostContext: () => () => {},
    onEmbedHostLocale: () => () => {},
    onEmbedHostToken: () => () => {},
  }
  let mounted
  const exports = {}
  new Function('require', 'exports', compiled)(name => {
    if (name === 'vue') return { ...vue, onMounted: fn => { mounted = fn }, onUnmounted: () => {} }
    if (name === 'vue-router') return { useRoute: () => ({ query: { token: 'ems_test' } }) }
    if (name === 'vue-i18n') return { useI18n: () => ({ locale: vue.ref('en'), t: key => key }) }
    if (name === '@/api/embed') return api
    if (name === '@/i18n/embed') return { readEmbedLocaleFromUrl: () => '' }
    throw new Error(`Unexpected import: ${name}`)
  }, exports)
  const bridge = exports.useEmbedBridge(vue.ref('channel'))
  mounted()
  for (let i = 0; i < 10; i++) await Promise.resolve()
  return { bridge, storage, creates: () => creates, ready: () => ready,
    fail: value => { fail = value }, finish: () => finishCreate() }
}

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
