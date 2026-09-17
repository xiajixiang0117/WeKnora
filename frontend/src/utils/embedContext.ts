/** Prefix host-injected context onto the user query for embed chat. */
export function buildQueryWithHostContext(
  query: string,
  hostContext?: Record<string, unknown>,
): string {
  if (!hostContext || !Object.keys(hostContext).length) return query
  const lines = Object.entries(hostContext)
    .filter(([, v]) => v !== undefined && v !== null && v !== '')
    .map(([k, v]) => `${k}: ${typeof v === 'string' ? v : JSON.stringify(v)}`)
  if (!lines.length) return query
  return `[Host context]\n${lines.join('\n')}\n\n${query}`
}

/** Restore the display text of persisted embed queries, keeping model context on the server. */
export function restoreEmbedMessageDisplay<T extends { role?: unknown; content?: unknown }>(
  message: T,
): T {
  if (message.role !== 'user' || typeof message.content !== 'string') return message
  // Only recognize the prefix emitted by buildQueryWithHostContext. Do not
  // remove context quoted later in a question or alter assistant responses.
  const prefix = /^\[Host context\]\n[^\n]+: [\s\S]*?\n\n/.exec(message.content)
  if (!prefix) return message
  return { ...message, content: message.content.slice(prefix[0].length) }
}
