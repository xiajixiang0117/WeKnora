import assert from 'node:assert/strict'
import test from 'node:test'

import {
  EMBED_PARENT_ORIGIN_HEADER,
  embedParentOriginHeaders,
  getEmbedParentOrigin,
  isTrustedEmbedParentMessage,
  resetEmbedParentOriginForTests,
} from './embedParentOrigin.ts'

function installFrame(referrer = '', topLevel = false) {
  const parent = {}
  const frameWindow: Record<string, unknown> = {
    location: { href: 'https://embed.example.com/embed/ch-1' },
  }
  frameWindow.parent = topLevel ? frameWindow : parent
  Object.defineProperty(globalThis, 'window', {
    value: frameWindow,
    configurable: true,
    writable: true,
  })
  Object.defineProperty(globalThis, 'document', {
    value: { referrer },
    configurable: true,
    writable: true,
  })
  return parent
}

test.afterEach(() => {
  resetEmbedParentOriginForTests()
})

test('uses the iframe referrer origin as the embed parent origin', () => {
  installFrame('https://shop.example.com/products/42')

  assert.equal(getEmbedParentOrigin(), 'https://shop.example.com')
  assert.deepEqual(embedParentOriginHeaders(), {
    [EMBED_PARENT_ORIGIN_HEADER]: 'https://shop.example.com',
  })
})

test('does not invent a parent origin for a top-level embed page', () => {
  installFrame('', true)

  assert.equal(getEmbedParentOrigin(), '')
  assert.deepEqual(embedParentOriginHeaders(), {})
})

test('pins the first trusted parent message origin when the referrer is absent', () => {
  const parent = installFrame()
  const first = {
    source: parent,
    origin: 'https://shop.example.com',
    data: { source: 'weknora-host', type: 'provide_token' },
  } as MessageEvent

  assert.equal(isTrustedEmbedParentMessage(first), true)
  assert.equal(getEmbedParentOrigin(), 'https://shop.example.com')

  const attacker = {
    ...first,
    origin: 'https://evil.example.com',
  } as MessageEvent
  assert.equal(isTrustedEmbedParentMessage(attacker), false)
})
