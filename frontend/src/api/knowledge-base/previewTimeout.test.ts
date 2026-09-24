import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'

const knowledgeApi = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
const request = readFileSync(new URL('../../utils/request.ts', import.meta.url), 'utf8')

test('knowledge file preview has a dedicated timeout without changing other Blob requests', () => {
  assert.match(knowledgeApi, /getDown\(`\/api\/v1\/knowledge\/\$\{id\}\/preview`, 300000\)/)
  assert.match(request, /getDown\(url: string, timeout\?: number\)/)
  assert.match(request, /timeout === undefined \? \{\} : \{ timeout \}/)
  assert.match(request, /timeout: 30000/)
})
