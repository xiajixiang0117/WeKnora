# Embed iframe 宿主 Origin 校验设计

> 历史设计说明（2026-09-20）：合并官方版本后，宿主限制改由页面 CSP `frame-ancestors` 实施，Embed API 使用官方同源鉴权策略。下文 `X-Embed-Parent-Origin` 服务端校验方案已被替代，不代表当前授权协议。恢复本地功能时保留这一官方方案，详见[修复笔记](../../../.agents/notes/implemented/bug-fix/2026-09-20-restore-fork-contracts.md)。

## 背景

网页 A 通过 iframe 加载 WeKnora 的 Embed 页面 B。现有 `allowed_origins` 同时被当作 API 请求 Origin 白名单和 iframe 宿主白名单使用；由于 iframe 内的 API 请求天然从 B 发出，默认前端 Nginx 部署实际上没有校验宿主 A。

## 目标

- 将 `allowed_origins` 的产品语义明确为允许嵌入的宿主页面 Origin（A）。
- 普通 iframe 接入时，A 未被允许则 Embed API 返回 403；B 不再要求手工加入渠道白名单。
- 保留安全模式服务端调用 `/exchange` 的兼容性：没有父页面头时，校验该服务端显式发送的 `Origin`。
- 不破坏管理端 Embed 预览；预览使用独立的短时预览会话路径。
- 覆盖跨域父页面识别、API 请求、CORS、后端鉴权和文档提示的回归测试。

## 方案与数据流

1. Embed 页面沿用已有的 `postMessage` 信任链；优先使用已验证的父窗口 Origin，初始化阶段没有消息时使用 iframe 的 `document.referrer` Origin。
2. Embed 前端为所有 `/api/v1/embed/:channel_id/...` 请求附加 `X-Embed-Parent-Origin`。
3. `EmbedAuth` 优先使用该头并以渠道 `allowed_origins` 校验。缺少该头时，仅对 `/exchange` 使用请求的 `Origin`，兼容安全模式的业务后端调用；其他公开 Embed API 缺少父 Origin 时拒绝。
4. `X-Embed-Parent-Origin` 只允许由 B 上的 iframe 通过同源 API 请求发送，不加入全局 CORS 允许头。否则任意第三方页面都能跨域伪造一个白名单内的 A。当前默认 Nginx、独立 Embed 子域和 Vite 开发代理均保持 B 与 `/api` 同源。
5. Go 直接托管前端时已有的 `frame-ancestors` 中间件继续使用同一宿主白名单，作为浏览器侧的额外防护；默认 Nginx 静态部署依赖 API 层父 Origin 校验。
6. 管理端预览继续走短时预览会话（`ems_preview_` 前缀）；后端只对该受管理的会话放行管理端父 Origin。Lite 模式的 `frame-ancestors` 始终包含 `'self'` 以允许同源管理预览，不使用可伪造的查询参数关闭 CSP。

## 兼容与迁移

当前环境只有一个旧渠道，其白名单值是 B。上线前将它直接修改为实际宿主 A；不做自动推断。后续新建和编辑界面均按“一行一个宿主 Origin”保存。

## 错误处理

- 未提供父 Origin、父 Origin 不在白名单、或 Origin 格式非法：返回现有的 403 `origin not allowed`。
- 安全模式 `/exchange` 仍可使用业务后端显式的 `Origin`；该值必须在渠道白名单中。
- 预览会话失败继续使用现有的 401/403 响应，不降级到发布 Token。

## 测试范围

- Go：父 Origin 命中、未命中、缺失；安全模式 `/exchange` 的 Origin 回退；CSP 头行为保持不变。
- 前端：父 Origin 的获取优先级、Embed API 请求头注入、跨域/同域场景。
- 文档与 UI：白名单说明改为宿主 A；普通 iframe 不再要求填 B，安全模式单独说明 exchange Origin。
