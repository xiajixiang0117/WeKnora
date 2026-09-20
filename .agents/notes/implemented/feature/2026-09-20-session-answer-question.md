# Agent Note: 会话回答明细展示原始问题

Status: implemented

## Problem

回答明细只有模型与用量，管理员难以判断每条回答对应哪次提问。

## Decision

管理员用量详情新增 question 字段，在已读取的会话消息中按非空 request_id 关联用户消息。遍历完成后关联，避免分页及消息排序影响。缺失关联时保留空值，不按相邻时间猜测。前端复用嵌入消息展示清理，表格省略长问题，所选回答上方以独立文本框展示完整问题。

## Alternatives considered

复用执行轨迹 original_query 可以只改前端，但没有轨迹的历史回答仍无法展示已保存的问题，因此使用用户消息作为来源。

## Consequences

多轮回答按请求准确匹配，缺失请求不误配；长问题单行省略，全文保留换行且安全转义。历史消息缺少 request_id 时不能恢复关联。新增字段只用于已有管理员接口，不改变普通聊天接口。读取详情时增加与用户问题总长度相关的临时内存和响应体积。

## Testing

前端生产构建与 verify-notes 校验通过。新增跨分页、乱序消息和缺失请求 ID 的关联回归测试。后端测试被仓库既有 KnowledgeListTitleRefreshPayload、TypeWebCrawlScan、TypeWebCrawlApply 类型缺失阻塞。浏览器访问本地会话管理入口会跳转登录页，尚未完成真实数据下的视觉验收。

## Related notes

已检索现有 proposed、implemented、rejected 笔记；当前三篇均为流程记录，与本改动无重叠。
