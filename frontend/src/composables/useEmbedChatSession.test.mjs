import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'
import * as vue from 'vue'

const compiled = ts.transpileModule(readFileSync(new URL('./useEmbedChatSession.ts', import.meta.url), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

function setup() {
  const sessionId = vue.ref('')
  const sessionSig = vue.ref('')
  const streams = []
  const notices = []
  let historyReads = 0
  let finish
  let fail = false
  let token = 'initial-token'
  const exports = {}
  const noop = () => {}
  new Function('require', 'exports', compiled)(name => {
    if (name === 'vue') return { ...vue, onMounted: noop, onUnmounted: noop }
    if (name === 'vue-i18n') return { useI18n: () => ({ t: key => key }) }
    if (name === '@/api/chat/streame') return { useStream: () => ({ onChunk: noop, error: vue.ref(''), startStream: async data => streams.push(data), stopStream: noop }) }
    if (name === '@/api/embed') return {
      getEmbedMessageList: async () => { historyReads++; return { data: [] } },
      postEmbedMessageSent: noop, postEmbedMessageReceived: noop, relayEmbedWebhookEvent: noop, stopEmbedSession: async () => {},
    }
    if (name === '@/utils/embedToast') return { embedToast: value => notices.push(value) }
    if (name === '@/utils/embedContext') return { buildQueryWithHostContext: value => value, restoreEmbedMessageDisplay: value => value }
    if (name === '@/utils/embedFile') return { fileToDataURI: noop }
    if (name === '@/composables/useStickyBottomOnResize') return { useStickyBottomOnResize: noop }
    if (name === '@/composables/useChatStreamHandler') return { useChatStreamHandler: () => ({ prepareForNewOutgoingMessage: noop, markInFlightAssistantStopped: noop }) }
    throw new Error(`Unexpected import: ${name}`)
  }, exports)
  const scope = vue.effectScope()
  const chat = scope.run(() => exports.useEmbedChatSession({
    sessionId, sessionSig, visitorId: vue.ref('visitor'), channelId: 'channel', get token() { return token }, agentId: 'agent', kbIds: [],
    ensureSession: async () => {
      if (fail) throw new Error('offline')
      await new Promise(resolve => { finish = resolve })
      token = 'renewed-token'
      sessionSig.value = 'signature'
      sessionId.value = 'created'
    },
  }))
  return { chat, streams, notices, scope, finish: () => finish(), fail: value => { fail = value }, historyReads: () => historyReads }
}

test('first send waits for a signed session without fetching history or dropping the user message', async () => {
  const h = setup()
  try {
    assert.equal(h.chat.historyLoading.value, false)
    const sending = h.chat.sendMsg('hello')
    assert.equal(h.chat.isReplying.value, true)
    assert.equal(h.streams.length, 0)
    await h.chat.sendMsg('duplicate')
    h.finish()
    await sending
    assert.equal(h.historyReads(), 0)
    assert.equal(h.chat.messagesList.length, 1)
    assert.equal(h.chat.messagesList[0].content, 'hello')
    assert.equal(h.chat.isReplying.value, true)
    assert.equal(h.streams.length, 1)
    assert.equal(h.streams[0].session_id, 'created')
    assert.equal(h.streams[0].embed_session_sig, 'signature')
    assert.equal(h.streams[0].embed_token, 'renewed-token')
  } finally { h.scope.stop() }
})

test('failed creation allows retry; stopping during creation does not send a message', async () => {
  const h = setup()
  try {
    h.fail(true)
    await h.chat.sendMsg('hello')
    assert.equal(h.chat.isReplying.value, false)
    assert.equal(h.chat.messagesList.length, 0)
    assert.equal(h.notices.length, 1)
    h.fail(false)
    const sending = h.chat.sendMsg('retry')
    h.chat.handleStopGeneration()
    h.finish()
    await sending
    assert.equal(h.streams.length, 0)
    assert.equal(h.chat.messagesList.length, 0)
  } finally { h.scope.stop() }
})
