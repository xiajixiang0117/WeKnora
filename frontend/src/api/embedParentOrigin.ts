/** Same-origin-only header carrying the browser-derived host page origin. */
export const EMBED_PARENT_ORIGIN_HEADER = 'X-Embed-Parent-Origin'

export const EMBED_HOST_SOURCE = 'weknora-host'

let verifiedParentOrigin = ''

function referrerParentOrigin(): string {
  if (typeof window === 'undefined' || window.parent === window) return ''
  try {
    if (document.referrer) {
      return new URL(document.referrer).origin
    }
  } catch {
    // Ignore malformed or unavailable referrers and wait for the handshake.
  }
  return ''
}

/** Best-known parent origin: verified handshake first, then referrer. */
export function getEmbedParentOrigin(): string {
  return verifiedParentOrigin || referrerParentOrigin()
}

/**
 * Validate a message from the embedding page and pin its origin for the rest
 * of this iframe's lifetime.
 */
export function isTrustedEmbedParentMessage(event: MessageEvent): boolean {
  if (typeof window === 'undefined' || window.parent === window) return false
  if (event.source !== window.parent) return false
  if (!event.data || event.data.source !== EMBED_HOST_SOURCE) return false
  if (typeof event.origin !== 'string' || event.origin === 'null') return false

  const expected = getEmbedParentOrigin()
  if (expected) {
    return event.origin === expected
  }
  verifiedParentOrigin = event.origin
  return true
}

/** Headers shared by every request made by the embedded iframe. */
export function embedParentOriginHeaders(): Record<string, string> {
  const origin = getEmbedParentOrigin()
  return origin ? { [EMBED_PARENT_ORIGIN_HEADER]: origin } : {}
}

/** Test-only reset for the module-level trust-on-first-use state. */
export function resetEmbedParentOriginForTests() {
  verifiedParentOrigin = ''
}
