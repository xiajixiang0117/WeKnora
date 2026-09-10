# Web Knowledge Citations Design

## Status

Approved for implementation on 2026-09-10.

## Problem

Pages imported through the web crawler are converted to Markdown before they
are indexed. The generated Markdown filename (for example, `PM示例.md`) is
currently carried through retrieval and rendered as a knowledge-document
citation. Although the knowledge record already retains its source URL, that
URL is not preserved across every citation boundary. Users consequently cannot
recognize or open the original web page from an answer citation.

## Goals

- Render an HTTP(S) page imported as `Knowledge.Type == "url"` as a web
  citation, backed by its original URL.
- Preserve internal chunk IDs for retrieval, access checks, de-duplication and
  source-drawer matching.
- Keep ordinary uploaded files, manual documents and direct file downloads as
  document citations.
- Work for semantic retrieval, keyword retrieval, Agent replies, ordinary
  knowledge-base chat, persisted references and the reference drawer.
- Let historical data continue to render safely when new source fields are
  absent.

## Non-goals

- Change Markdown conversion, chunking, crawler storage paths or URL fetching.
- Treat live web search results and stored web knowledge as the same retrieval
  type. They remain separate internally.
- Rewrite citations in assistant messages that have already been persisted.

## Design

### Source classification

`types.SearchResult` will carry the owning knowledge's `KnowledgeType` in
addition to its existing `KnowledgeSource`. Search-result builders populate
both fields from the knowledge row.

Only a result with all of the following is a stored web page:

1. `KnowledgeType == "url"`;
2. `KnowledgeSource` is a valid `http` or `https` URL.

This excludes direct-file imports such as a PDF downloaded from a URL, whose
knowledge type is not `url`.

### Citation protocol

The model continues to see and cite the retrieved chunk with its private `cN`
handle. `ChunkReference` gains the page classification and source URL. When a
valid cited `cN` is expanded for the user:

- A normal chunk expands to the existing `<kb ... />` tag.
- A stored-page chunk expands to `<web url="original URL" title="document title" />`.

Keeping `cN` private avoids changing Agent tool contracts, chunk lookup and
deduplication. The public citation is transformed deterministically, so the
model never needs to reproduce a long URL.

### Data propagation

The following result boundaries retain `knowledge_type` and
`knowledge_source`:

- Semantic `knowledge_search` results.
- Keyword `grep_chunks` results, including its SQL projection and structured
  result rows.
- Agent reference reconstruction from tool-result data.
- The ordinary chat pipeline's model-context registration.

Missing fields are treated as a normal document source for backward
compatibility.

### Frontend presentation

Reference data gains optional `knowledge_type` and `knowledge_source` fields.
The reference drawer considers a stored-page result to be a web source under
the same condition used by the backend. It uses `knowledge_source` as the URL
when the result ID is a private chunk ID.

The answer citation uses the existing web appearance: a web icon and domain;
its hover information exposes the complete URL. The reference drawer lists it
in the web-source section and its card links to the original page. The UI must
not use an internally generated `.md` filename as the primary page-source
label. If an old crawler record still has such a title, the domain/URL is used
instead.

### Crawler titles

Crawler pages retain their generated `.md` name only as an internal file name.
When a crawler item is recorded as URL-origin knowledge, its display title is
set to the extracted page title. A later crawler refresh detects a title
mismatch and repairs existing crawler records. New replies can still show the
correct URL for old records before they are refreshed.

## Failure and Security Handling

- URLs are rendered as web sources only when they pass standard HTTP(S) URL
  parsing. Invalid, empty and non-HTTP schemes fall back to document behavior.
- The server remains the authority for chunk content and document access.
  Changing the public citation tag does not expose storage paths or bypass
  existing authorization checks.
- An unresolved source URL never creates a fabricated web link.

## Verification

- Backend unit tests cover stored-page `cN` expansion to `<web>`, normal
  document expansion to `<kb>`, and invalid/file URL fallback behavior.
- Agent tests verify semantic and keyword tool rows preserve the source fields
  into durable `knowledge_references`.
- Chat-pipeline tests verify URL knowledge is rendered as a web citation while
  a URL-hosted PDF remains a document citation.
- Frontend tests verify stored-page references group as web, target the source
  URL, and do not display a generated `.md` filename as the source label.
- Relevant Go and frontend test suites must pass before delivery.
