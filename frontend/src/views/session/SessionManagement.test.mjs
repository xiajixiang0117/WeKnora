import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { createRequire } from "node:module"
import test from "node:test"
import ts from "typescript"
import { compileScript, parse } from "@vue/compiler-sfc"
import { createSSRApp, h, ref } from "vue"
import { renderToString } from "@vue/server-renderer"

const require = createRequire(import.meta.url)
function component(filename, inlineTemplate = true, api = {}) {
  const { descriptor } = parse(readFileSync(new URL(filename, import.meta.url), "utf8"))
  const compiled = compileScript(descriptor, { id: filename, inlineTemplate })
  const code = ts.transpileModule(compiled.content, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const exports = {}
  new Function("require", "exports", code)(name => {
    if (name === "vue-i18n") return { useI18n: () => ({ t: key => key, locale: ref("en-US") }) }
    if (name === "vue") return { ...require("vue"), onMounted: () => {} }
    if (name === "@/api/session-usage") return api
    if (name === "@/utils/embedContext") return { restoreEmbedMessageDisplay: value => value }
    if (name.endsWith(".vue")) return { default: {} }
    return require(name)
  }, exports)
  return exports.default
}

const Candidates = component("./TraceCandidates.vue")
const Answer = component("./TraceAnswer.vue")
async function render(component, props) { return renderToString(createSSRApp({ render: () => h(component, props) })) }

test("chunk expansion includes full escaped Unicode content, identity, provenance and scores", async () => {
  const content = "完整片段\n".repeat(1000) + "<script>alert(1)</script>"
  const html = await render(Candidates, { scores: "rerank", candidates: [{
    chunk_id: "chunk-1", knowledge_id: "doc-1", knowledge_name: "Document", content,
    content_source: "answer_reference", retrieval_score: 0.9, model_score: 0.8, final_score: 0.85, selected: true,
  }] })
  assert.ok(html.includes("<details"))
  assert.ok(html.includes("doc-1"))
  assert.ok(html.includes("0.8500"))
  assert.equal(html.split("完整片段").length - 1, 1000)
  assert.ok(html.includes("&lt;script&gt;"))
  assert.ok(!html.includes("<script>"))
  assert.ok(html.includes("sessionManagement.referenceContentNote"))
})

test("legacy missing chunk text and unfinished/fallback answers have explicit states", async () => {
  assert.ok((await render(Candidates, { candidates: [{ chunk_id: "old", retrieval_score: 0, selected: false }] })).includes("sessionManagement.chunkUnavailable"))
  const html = await render(Answer, { content: "partial <img onerror=alert(1)>", completed: false, fallback: true })
  assert.ok(html.includes("sessionManagement.partialAnswer"))
  assert.ok(html.includes("sessionManagement.fallbackAnswer"))
  assert.ok(html.includes("&lt;img"))
  assert.ok((await render(Answer, { content: "", completed: true })).includes("sessionManagement.noAnswer"))
})

function deferred() {
  let resolve, reject
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

test("changing trace selection ignores stale responses and failures", async () => {
  const calls = []
  const page = component("./SessionManagement.vue", false, {
    getRetrievalExecutionTrace: () => { const next = deferred(); calls.push(next); return next.promise },
  }).setup({}, { expose: () => {} })
  page.selectedSummary.value = { session_id: "session" }
  const first = page.openTrace({ request_id: "first" })
  const second = page.openTrace({ request_id: "second" })
  calls[1].resolve({ data: { request_id: "second" } })
  await second
  calls[0].reject(new Error("stale failure"))
  await first
  assert.equal(page.selectedTrace.value.request_id, "second")
  assert.equal(page.traceError.value, "")
  assert.equal(page.traceLoading.value, false)
  const third = page.openTrace({ request_id: "third" })
  assert.equal(page.selectedTrace.value, null)
  assert.equal(page.traceLoading.value, true)
  calls[2].reject(new Error("load failed"))
  await third
  assert.equal(page.traceError.value, "load failed")
})

test("opening another session clears answer and trace and prevents late detail replacement", async () => {
  const calls = []
  const page = component("./SessionManagement.vue", false, {
    getSessionUsage: () => { const next = deferred(); calls.push(next); return next.promise },
    listRetrievalExecutionTraces: async () => ({ data: [] }),
  }).setup({}, { expose: () => {} })
  page.selectedAnswer.value = { content: "old answer" }
  page.selectedTrace.value = { request_id: "old" }
  const first = page.openDetails({ session_id: "first" })
  const second = page.openDetails({ session_id: "second" })
  assert.equal(page.selectedAnswer.value, null)
  assert.equal(page.selectedTrace.value, null)
  calls[1].resolve({ data: { summary: { session_id: "second" }, details: [{ content: "current" }] } })
  await second
  calls[0].resolve({ data: { summary: { session_id: "first" }, details: [{ content: "stale" }] } })
  await first
  assert.equal(page.selectedSummary.value.session_id, "second")
  assert.equal(page.details.value[0].content, "current")
})
