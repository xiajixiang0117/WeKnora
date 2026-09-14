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

export interface RetrievalTraceSummary {
  request_id: string
  user_message_id?: string
  assistant_message_id?: string
  status: string
  original_query: string
  step_count: number
  created_at: string
}

export interface RetrievalTraceCandidate {
  knowledge_base_id?: string
  knowledge_base_name?: string
  knowledge_id?: string
  knowledge_name?: string
  chunk_id: string
  retrieval_score: number
  model_score?: number
  final_score?: number
  selected: boolean
}

export interface RetrievalTraceStep {
  sequence: number
  kind: string
  source?: string
  status: string
  created_at: string
  query?: string
  rewritten_query?: string
  expansion_queries?: string[]
  retrieval?: {
    knowledge_base_id?: string
    knowledge_base_name?: string
    returned_count: number
    captured_count: number
    truncated?: boolean
    candidates?: RetrievalTraceCandidate[]
  }
  rerank?: {
    model_id?: string
    model_name?: string
    threshold: number
    candidate_count: number
    captured_count: number
    truncated?: boolean
    candidates?: RetrievalTraceCandidate[]
  }
  error_summary?: string
}

export interface RetrievalExecutionTrace {
  id: string
  request_id: string
  user_message_id?: string
  assistant_message_id?: string
  status: string
  created_at: string
  trace: {
    trace_version: number
    original_query: string
    steps: RetrievalTraceStep[]
  }
}

export interface RetrievalTraceListResponse {
  success: boolean
  data: RetrievalTraceSummary[]
}

export interface RetrievalTraceResponse {
  success: boolean
  data: RetrievalExecutionTrace
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

export function listRetrievalExecutionTraces(sessionId: string) {
  return get<RetrievalTraceListResponse>(`/api/v1/sessions/${encodeURIComponent(sessionId)}/retrieval-execution-traces`)
}

export function getRetrievalExecutionTrace(sessionId: string, requestId: string) {
  return get<RetrievalTraceResponse>(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/retrieval-execution-traces/${encodeURIComponent(requestId)}`,
  )
}
