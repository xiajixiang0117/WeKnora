# Agent Note: 禁止推送自动触发工作流

Status: implemented

## Problem

合并上游后，推送 main 会启动 App 和笔记校验。维护者要求所有 push 都不自动触发 Action，包括向已有 PR 分支推送提交。

## Decision

移除 App 和 Verify Agent Notes 的 push、pull_request 入口，保留 workflow_dispatch。其余工作流继续只允许手动启动或 workflow_call 调用，构建与部署步骤不变。

## Existing note audit

[笔记校验决策](2026-09-18-notes-validation.md) 部分重叠：本决定取代自动触发策略，保留其工具链和校验设计，因此不归档旧篇并互链。[上游集成提案](../../proposed/process/2026-09-18-upstream-integration.md) 涉及合并与迁移，不拥有触发策略，保留原状。

## Alternatives considered

- 只删除 push 入口：改动最少并保留 PR 自动校验，但 PR 的 synchronize 事件仍会由推送触发，不满足要求。
- 在仓库设置禁用全部 Actions：能统一阻断自动执行，但也会阻断现有手动发布和部署；保留手动入口更符合现有运维流程。

## Testing

- 解析全部工作流，确认入口仅有 workflow_dispatch 或 workflow_call。
- npm run verify-notes 和 git diff --check 通过。

## Consequences

推送不再消耗自动运行资源，手动校验和发布入口仍可用。推送和 PR 不再自动提供检查结果，需要维护者手动执行。旧分支或旧标签携带的历史工作流不受当前修改覆盖，使用前需同步此配置；未来合并上游也需重新核对触发条件。
