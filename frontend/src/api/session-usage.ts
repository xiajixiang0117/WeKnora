import { get } from '@/utils/request'

export interface SessionTokenUsage {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cache_read_tokens?: number
  cache_write_tokens?: number
  cache_miss_tokens?: number
}

export interface SessionUsageAgent {
  id: string
  name?: string
}

export type SessionUsageSource = 'web' | 'embed' | 'api'

export interface SessionUsageSummary {
  session_id: string
  title: string
  channel: string
  channel_id?: string
  updated_at: string
  answer_count: number
  tracked_answer_count: number
  agents: SessionUsageAgent[]
  usage: SessionTokenUsage
}

export interface SessionUsageDetail {
  message_id: string
  request_id: string
  created_at: string
  agent_id?: string
  agent_name?: string
  model_id?: string
  model_name?: string
  has_usage: boolean
  usage?: SessionTokenUsage
}

export interface SessionUsageListResponse {
  success: boolean
  data: SessionUsageSummary[]
  total: number
  page: number
  page_size: number
}

export interface SessionUsageDetailsResponse {
  success: boolean
  data: {
    summary: SessionUsageSummary
    details: SessionUsageDetail[]
  }
}

export function listSessionUsage(params: {
  page: number
  pageSize: number
  keyword?: string
  source?: SessionUsageSource
}) {
  const query = new URLSearchParams({
    page: String(params.page),
    page_size: String(params.pageSize),
  })
  if (params.keyword?.trim()) query.set('keyword', params.keyword.trim())
  if (params.source) query.set('source', params.source)
  return get<SessionUsageListResponse>(`/api/v1/sessions/usage?${query.toString()}`)
}

export function getSessionUsage(sessionId: string) {
  return get<SessionUsageDetailsResponse>(`/api/v1/sessions/${encodeURIComponent(sessionId)}/usage`)
}
