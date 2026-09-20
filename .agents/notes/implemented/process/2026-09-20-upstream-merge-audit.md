# Agent Note: 官方合并本地改动保留审计

Status: implemented

## Problem

2026-09-18 合并官方代码后，会话管理显示翻译键。用户要求追溯并检查其他本地改动是否遗漏。本记录审计 Git 源码，不代表共享服务器正在运行同一提交。

## Decision

按共同祖先、本地合并前、官方合并前、合并结果、当前 HEAD 五个状态核对，结合调用链和已有测试区分真正遗漏与官方等价实现。以下问题和失败结果是修复前的审计基线；后续恢复结果见[修复笔记](../bug-fix/2026-09-20-restore-fork-contracts.md)。未部署。

- 共同祖先：`85b76d1a3012df37377065aaad21eff1578b6506`。
- 本地合并前：`00c2a17`。
- 官方合并前：`92e7c0a`。
- 出现遗漏的集成提交：`1493de3388c4cb8427146e069b84cdc5b29e26d7`，2026-09-18 14:34:32 +0800。
- 进入 main：`df4586929d751f6403fcf636030321afaea7d1e9`，2026-09-18 14:48:28 +0800。
- 审计 HEAD：`7c592f5`；此前 `e65c6cf` 新增问题展示，`7c592f5` 恢复会话管理翻译。

共同祖先到本地父提交共有 227 个路径变化（含删除路径，不能用默认重命名合并后的 diff 文件数代替）。128 个路径在集成提交与本地父提交一致；37 个与官方父提交一致；62 个为组合修改。37 是文件分类数量，不是缺陷数量。组合文件的本地新增非空长行也进行了逐行缺失扫描，候选主要为样式变化和旧工具名称迁移；这类文本扫描不是语义正确性的证明。

## 已确认的问题

### P1：后台任务契约丢失，当前后端不能编译

`internal/types/task.go` 整体采用官方版本，丢失 `TypeWebCrawlScan`、`TypeWebCrawlApply`、`TypeKnowledgeListTitleRefresh`、`KnowledgeListTitleRefreshPayload` 及队列归属。调用方仍在：

- `internal/application/service/knowledge_title_refresh.go:22`
- `internal/application/service/web_crawler_service.go:48`、148
- `internal/router/task.go:312` 和 `internal/router/sync_task.go:160`

Go 检查实际报告前三项 undefined。仅补常量不够，还需恢复队列映射和 payload。

### P1：执行轨迹入口、保存和清理链路丢失

`internal/handler/session/handler.go` 丢失 traceStore 字段、构造注入及删除/清空会话时的轨迹清理。`retrieval_execution_trace.go:26` 等位置仍引用 h.traceStore，是源码可直接确认的未定义字段；当前 Go 检查先被 service 包编译错误阻塞，未运行到此处。

`internal/handler/session/qa.go` 丢失 recorder 创建、异步上下文传递以及完成/失败/取消时的 finishTrace 保存。`internal/agent/engine.go` 丢失 recordToolGenerationContext 调用；函数仍在 references.go，但生产调用已不存在。只保留轨迹表、查询接口和采集辅助函数不能让新请求形成完整记录。

### P1：嵌入聊天延迟建会话接线断开

`useEmbedBridge.ts:161` 在首次访问时保持空 sessionId，等待首次发送时 ensureSession 创建。`EmbedPage.vue:29` 却仍要求 sessionId 非空才渲染聊天，导致无历史会话时不能进入可发送状态；页面也没有向 EmbedChatView 传 ensureSession。`EmbedChatCore.vue:276` 调用 composable 时同样漏传。

vue-tsc 实际报告 EmbedPage.vue:28、EmbedChatCore.vue:276 两处缺少 ensureSession。composable 独立测试通过并不覆盖组件接线。

### P1：网站抓取配置与审核入口丢失

`DataSourceEditorDialog.vue` 丢失 web_crawler 类型、种子 URL/范围/CSS 选择器配置、跳过凭据步骤和创建后扫描逻辑。`DataSourceSettings.vue` 丢失网站手动同步分支、检查更新入口、审核抽屉接入。WebCrawlReviewDrawer、API、后端服务与迁移文件仍保留，不能据此认为用户流程仍可用。

原有 DataSourceEditorDialog.web-crawler.test.mjs 失败；DataSourceSettings.test.ts 三项失败（手动同步、长错误文本、翻译）。

### P2：主应用仍缺网站抓取和网页解析翻译

每种语言的 146 个本地变化键中，集成提交缺少 145 个；当前五种语言各缺 61 个。包括 datasource 下 51 个网站抓取相关键、uploadConfirm 下 8 个网页提取规则键，以及 2 个未使用的旧 sessionManagement 列键（document、selected）。不能把两个未使用键算成当前可见故障。

`UploadConfirmDialog.vue:203` 等仍直接使用 webRulesTitle、webContentSelectorLabel 等缺失键。会话管理修复只处理了 sessionManagement，没有覆盖这些命名空间。

### P2：Agent 最终来源归集与本地提示规则丢失

`internal/agent/finalize.go` 丢失 emitCompletionEvent 前的 collectKnowledgeReferences(state)。函数及其测试仍保留，却没有生产调用，最终完成事件不再通过本地逻辑从实际引用筛选和补齐知识来源。

同文件的 finalAnswerCitationRequirement 和自然行文规则也消失；`internal/modelcontext/registry.go` 的不要反斜杠转义 Markdown/引用标记提示被覆盖。这是本地增强丢失，不等于官方所有引用能力失效，也不保证每次模型输出都会出现错误。

### P2：数据库和上下文分类验证回退

`internal/database/migration_sqlite_versioned_schema_test.go:51` 沿用官方预期版本 23，而迁移后的版本为 26。三个 SQLite 迁移测试均因 23 != 26 失败。该文件还丢失 web_crawl_pages、web_crawl_scans、web_crawl_changes、retrieval_execution_traces 的表覆盖。这证明测试未完成合并适配，不足以证明数据库迁移损坏。

`internal/types/context_clone.go` 丢失 SessionManagementReadContextKey 的显式 false 分类。`TestEveryContextKeyDeclaresACloneDecision` 实际失败；未将此缺项推断为权限绕过。

### P3：部分嵌入样式和窄屏保护被覆盖

EmbedInputField 丢失本地两行最小高度、按钮 disabled 等调整；EmbedUserMessage 的本地排版消失。不将所有样式差异视作功能故障。复核更正：两个 citation popover 的窄屏宽度与左边界约束已迁入共享 useCitationPopover，并非遗漏。

## 已排除或需要区别对待

- `frontend/src/i18n/embed.ts` 与官方完全相同，但官方已有 conversationTime、referencesDrawer、common.close；现有嵌入翻译四项测试通过，不能报这些翻译丢失。
- 旧 knowledge_search/list_knowledge_chunks 的改动部分迁入 search_knowledge/read_document：检索及重排记录、knowledge_source/type/filename 信息仍存在。旧文件被覆盖不代表新工具同样缺失。
- `internal/router/routes_agent.go` 已通过 embedpolicy.FrameAncestors 保留 'self'，管理端同源框架支持不能简单认定丢失。
- embed_auth 不再读取 X-Embed-Parent-Origin，而使用 CSP 约束父页面和同源 API 放行。这是官方替代方案；客户端和部分本地测试仍表达旧契约，需要统一并做代理/管理预览回归，尚不能仅据 diff 判为跨域不可用或安全漏洞。
- routerCORSConfig 辅助函数消失但原有请求头策略仍以内联配置存在；router_cors_test.go 仍引用不存在的辅助函数，属于测试接线问题。
- `internal/handler/embed_channel.go` 的本地差异只是注释。
- 工作流自动触发行为已在后续明确的 `07d4e3c` 中调整，按该决定评价，不能直接按合并前文件还原。
- 引用 Markdown 渲染、来源列表、问题展示等现有定向测试通过；这不覆盖后端实时来源生成。

## 验证

- `go test ./internal/types ./internal/modelcontext ./internal/handler/session ./internal/router`：modelcontext 通过；types 上下文分类测试失败；handler/router 被缺失任务定义阻塞。
- `npm run type-check`（frontend）：失败，两处 ensureSession 缺失。
- 第一组前端定向测试：11 项，10 通过、1 失败，覆盖 bridge、chat session、抓取编辑器、引用 CSS 和会话管理。
- 第二组前端定向测试：78 项，75 通过、3 失败，覆盖数据源设置、嵌入翻译/父域请求头、Markdown、知识轨迹和来源列表。
- `go test ./internal/database -run TestSQLiteMigrations -count=1`：三个测试失败，版本断言 23/26。
- 未操作服务器或生产数据库，未把当前代码的失败推断为服务器当前故障。
- 尚未完成全部后端包测试及浏览器端到端测试：当前编译错误阻塞完整验证。恢复契约后需再次检查深层错误。

## 历史笔记归属

[原合并提案](../../proposed/process/2026-09-18-upstream-integration.md) 部分重叠，保留其迁移编号与合并策略；本篇记录实际审计证据，不把原提案中列出的验收标准当成已通过结果。
[手动工作流决定](2026-09-18-manual-actions.md) 独立保留。
[会话问题展示与翻译修复](../feature/2026-09-20-session-answer-question.md) 只覆盖其明确范围，不扩展为全仓恢复证明。

## Alternatives considered

整体恢复 37 个文件为合并前版本最容易重现本地代码，但会同时丢弃官方安全、工具重构和其他更新，还会误恢复已有等价实现。故审计以功能契约为单位提出恢复目标，不执行整文件回退。

只按本地新增行是否还存在检查速度快，适合生成候选；但无法发现函数仍在而调用消失、组件 props 未传递等问题，必须结合调用链和现有测试。

## Consequences

已经确认这次集成存在实质遗漏，国际化只是其中一部分。优先恢复后端编译契约和轨迹依赖，再恢复嵌入组件接线、网站抓取入口和剩余翻译，随后统一新旧鉴权测试与数据库版本检查。这里的排序是恢复建议，本次没有将业务修复伪装成已完成。

下列清单覆盖全部 227 个本地变化路径，分类按合并时的 Git 对象内容，不是逐文件运行验收结论。

## 与官方版本一致（37）

- `.github/workflows/app.yml`
- `docs/embed-secure-mode.md`
- `docs/embed-subdomain.md`
- `frontend/src/components/EmbedInputField.vue`
- `frontend/src/composables/useChatCitationPopover.ts`
- `frontend/src/composables/useEmbedCitationPopover.ts`
- `frontend/src/i18n/embed.ts`
- `frontend/src/i18n/locales/en-US.ts`
- `frontend/src/i18n/locales/ja-JP.ts`
- `frontend/src/i18n/locales/ko-KR.ts`
- `frontend/src/i18n/locales/ru-RU.ts`
- `frontend/src/i18n/locales/zh-CN.ts`
- `frontend/src/views/embed/EmbedChatCore.vue`
- `frontend/src/views/embed/EmbedPage.vue`
- `frontend/src/views/embed/EmbedUserMessage.vue`
- `frontend/src/views/knowledge/settings/DataSourceEditorDialog.vue`
- `frontend/src/views/knowledge/settings/DataSourceSettings.vue`
- `internal/agent/engine.go`
- `internal/agent/finalize.go`
- `internal/agent/finalize_image_requirement_test.go`
- `internal/agent/tools/grep_chunks.go`
- `internal/agent/tools/grep_chunks_aggregate_test.go`
- `internal/agent/tools/knowledge_search.go`
- `internal/agent/tools/list_knowledge_chunks.go`
- `internal/application/service/embed_session_test.go`
- `internal/database/migration_sqlite_versioned_schema_test.go`
- `internal/handler/embed_channel.go`
- `internal/handler/session/artifact_completion_test.go`
- `internal/handler/session/handler.go`
- `internal/handler/session/qa.go`
- `internal/middleware/embed_auth.go`
- `internal/modelcontext/registry.go`
- `internal/router/router.go`
- `internal/router/routes_agent.go`
- `internal/types/context_clone.go`
- `internal/types/task.go`
- `internal/types/task_queue_test.go`

## 组合修改（62）

- `.dockerignore`
- `.github/workflows/docker-image.yml`
- `.github/workflows/docreader.yml`
- `.gitignore`
- `docker-compose.yml`
- `docker/Dockerfile.app`
- `docreader/parser/web_parser.py`
- `docs/QA.md`
- `frontend/src/api/chat/streame.ts`
- `frontend/src/api/datasource/index.ts`
- `frontend/src/components/AgentEmbedChannelPanel.vue`
- `frontend/src/components/ChatReferencesDrawer.vue`
- `frontend/src/components/EmbedChannelPreview.vue`
- `frontend/src/components/UserMenu.vue`
- `frontend/src/components/css/chat-citations.less`
- `frontend/src/components/css/chat-markdown.less`
- `frontend/src/composables/useEmbedChatSession.ts`
- `frontend/src/router/index.ts`
- `frontend/src/utils/request.ts`
- `frontend/src/views/chat/components/AgentStreamDisplay.vue`
- `frontend/src/views/chat/components/docInfo.vue`
- `frontend/src/views/embed/EmbedBotMessage.vue`
- `frontend/src/views/knowledge/KnowledgeBase.vue`
- `frontend/src/views/knowledge/components/UploadConfirmDialog.vue`
- `internal/agent/engine_test.go`
- `internal/agent/think.go`
- `internal/application/repository/session.go`
- `internal/application/service/chat_pipeline/chat_completion.go`
- `internal/application/service/chat_pipeline/chat_completion_stream.go`
- `internal/application/service/chat_pipeline/references.go`
- `internal/application/service/datasource_service.go`
- `internal/application/service/embed_session.go`
- `internal/application/service/knowledge_create.go`
- `internal/application/service/knowledge_create_test.go`
- `internal/application/service/knowledge_process.go`
- `internal/application/service/knowledgebase_search_results.go`
- `internal/application/service/session.go`
- `internal/container/container.go`
- `internal/datasource/connector.go`
- `internal/handler/knowledge.go`
- `internal/handler/session/agent_stream_handler.go`
- `internal/handler/session/helpers.go`
- `internal/middleware/embed_auth_test.go`
- `internal/modelcontext/citations.go`
- `internal/modelcontext/model_output.go`
- `internal/modelcontext/sources.go`
- `internal/modelcontext/sources_test.go`
- `internal/modelcontext/stream.go`
- `internal/router/routes_chat.go`
- `internal/router/routes_infra.go`
- `internal/router/routes_knowledge.go`
- `internal/router/sync_task.go`
- `internal/router/task.go`
- `internal/types/const.go`
- `internal/types/interfaces/knowledge.go`
- `internal/types/session.go`
- `website-docs/06-development/02-database-schema.md`
- `frontend/src/views/knowledge/settings/WebCrawlReviewDrawer.vue`
- `frontend/src/views/session/SessionManagement.vue`
- `frontend/src/views/session/TraceAnswer.vue`
- `frontend/src/views/session/TraceCandidates.vue`
- `internal/agent/references.go`

## 与本地父提交一致（128）

- `.github/pull_request_template.md`
- `.github/workflows/anydoc.yml`
- `.github/workflows/cli-e2e.yml`
- `.github/workflows/cli.yml`
- `.github/workflows/dsh-plugin.yml`
- `.github/workflows/frontend.yml`
- `.github/workflows/go-lint-cache.yml`
- `.github/workflows/go-lint.yml`
- `.github/workflows/mcp-server.yml`
- `docker/Dockerfile.docreader`
- `docreader/parser/parser.py`
- `docreader/tests/test_web_parser.py`
- `docs/mcp-tool-directory.md`
- `frontend/public/weknora-widget.js`
- `frontend/src/api/embed/index.ts`
- `frontend/src/composables/useEmbedBridge.ts`
- `frontend/src/stores/uploadConfirm.ts`
- `frontend/src/utils/chatMarkdownRenderer.test.ts`
- `frontend/src/utils/chatMarkdownRenderer.ts`
- `frontend/src/utils/citationMarkdown.ts`
- `frontend/src/utils/embedContext.ts`
- `frontend/src/utils/protectedFileAccess.test.ts`
- `frontend/src/utils/protectedFileAccess.ts`
- `frontend/src/utils/referenceSources.test.mjs`
- `frontend/src/utils/referenceSources.ts`
- `frontend/src/views/embed/EmbedChatView.vue`
- `frontend/src/views/knowledge/components/UploadConfirmDialog.test.ts`
- `internal/application/repository/datasource_repo.go`
- `internal/application/repository/datasource_repo_test.go`
- `internal/application/repository/session_test.go`
- `internal/application/service/chat_pipeline/query_expansion.go`
- `internal/application/service/chat_pipeline/query_understand.go`
- `internal/application/service/chat_pipeline/references_test.go`
- `internal/application/service/chat_pipeline/rerank.go`
- `internal/application/service/chat_pipeline/search.go`
- `internal/application/service/chat_pipeline/search_entity.go`
- `internal/application/service/datasource_sweep_wiring_test.go`
- `internal/application/service/session_user_scope_test.go`
- `internal/datasource/scheduler.go`
- `internal/datasource/scheduler_test.go`
- `internal/modelcontext/resources.go`
- `internal/types/context_helpers.go`
- `internal/types/datasource.go`
- `internal/types/datasource_test.go`
- `internal/types/interfaces/datasource.go`
- `internal/types/search.go`
- `migrations/sqlite/000013_mcp_tool_enabled.down.sql`
- `migrations/sqlite/000013_mcp_tool_enabled.up.sql`
- `migrations/versioned/000091_mcp_tool_enabled.down.sql`
- `migrations/versioned/000091_mcp_tool_enabled.up.sql`
- `migrations/versioned/000092_mcp_metadata.down.sql`
- `migrations/versioned/000092_mcp_metadata.up.sql`
- `package-lock.json`
- `.agents/notes/implemented/process/2026-09-18-notes-validation.md`
- `.github/workflows/deploy-frontend.yml`
- `.github/workflows/manual-ci-cd.yml`
- `.github/workflows/manual-registry-deploy.yml`
- `.github/workflows/manual-source-deploy.yml`
- `.github/workflows/verify-notes.yml`
- `deploy/registry-compose-deploy.sh`
- `deploy/source-compose-deploy.sh`
- `deploy/weknora-github-runner.service`
- `docs/AGENT_NOTES.md`
- `docs/MANUAL_SOURCE_DEPLOY.md`
- `docs/superpowers/plans/2026-09-14-session-retrieval-execution-traces.md`
- `docs/superpowers/specs/2026-09-03-website-sync-design.md`
- `docs/superpowers/specs/2026-09-10-web-knowledge-citations-design.md`
- `docs/superpowers/specs/2026-09-14-session-retrieval-execution-trace-design.md`
- `docs/superpowers/specs/2026-09-15-embed-host-origin-design.md`
- `frontend/public/weknora-embed-test.html`
- `frontend/src/api/embedParentOrigin.test.ts`
- `frontend/src/api/embedParentOrigin.ts`
- `frontend/src/api/session-usage.ts`
- `frontend/src/composables/useEmbedBridge.test.mjs`
- `frontend/src/composables/useEmbedChatSession.test.mjs`
- `frontend/src/utils/embedContext.test.ts`
- `frontend/src/views/knowledge/settings/DataSourceEditorDialog.web-crawler.test.mjs`
- `frontend/src/views/knowledge/settings/DataSourceSettings.test.ts`
- `frontend/src/views/session/SessionManagement.test.mjs`
- `internal/agent/references_test.go`
- `internal/application/repository/web_crawler.go`
- `internal/application/repository/web_crawler_test.go`
- `internal/application/service/knowledge_title_refresh.go`
- `internal/application/service/knowledge_url_title_test.go`
- `internal/application/service/web_crawler_delete.go`
- `internal/application/service/web_crawler_delete_test.go`
- `internal/application/service/web_crawler_missing_test.go`
- `internal/application/service/web_crawler_service.go`
- `internal/application/service/web_crawler_service_test.go`
- `internal/datasource/connector/webcrawler/connector.go`
- `internal/datasource/connector/webcrawler/connector_test.go`
- `internal/handler/datasource_web_crawler.go`
- `internal/handler/session/retrieval_execution_trace.go`
- `internal/handler/session/retrieval_execution_trace_test.go`
- `internal/handler/session/usage_management.go`
- `internal/handler/session/usage_management_test.go`
- `internal/modelcontext/escaped_resources_test.go`
- `internal/retrievaltrace/content_test.go`
- `internal/retrievaltrace/store.go`
- `internal/retrievaltrace/trace.go`
- `internal/retrievaltrace/trace_test.go`
- `internal/router/router_cors_test.go`
- `internal/webtitle/fetch.go`
- `internal/webtitle/fetch_test.go`
- `migrations/sqlite/000013_web_crawler_sync.down.sql`
- `migrations/sqlite/000013_web_crawler_sync.up.sql`
- `migrations/sqlite/000014_web_crawl_folder_path.down.sql`
- `migrations/sqlite/000014_web_crawl_folder_path.up.sql`
- `migrations/sqlite/000015_mcp_tool_enabled.down.sql`
- `migrations/sqlite/000015_mcp_tool_enabled.up.sql`
- `migrations/sqlite/000016_retrieval_execution_traces.down.sql`
- `migrations/sqlite/000016_retrieval_execution_traces.up.sql`
- `migrations/versioned/000091_web_crawler_sync.down.sql`
- `migrations/versioned/000091_web_crawler_sync.up.sql`
- `migrations/versioned/000092_web_crawl_folder_path.down.sql`
- `migrations/versioned/000092_web_crawl_folder_path.up.sql`
- `migrations/versioned/000093_mcp_tool_enabled.down.sql`
- `migrations/versioned/000093_mcp_tool_enabled.up.sql`
- `migrations/versioned/000094_mcp_metadata.down.sql`
- `migrations/versioned/000094_mcp_metadata.up.sql`
- `migrations/versioned/000095_retrieval_execution_traces.down.sql`
- `migrations/versioned/000095_retrieval_execution_traces.up.sql`
- `package.json`
- `scripts/agent-note-tree.ts`
- `scripts/archive-agent-note.ts`
- `scripts/verify-agent-note-format.ts`
- `scripts/verify-agent-note-tree.ts`
- `scripts/verify-archived-agent-notes.ts`
