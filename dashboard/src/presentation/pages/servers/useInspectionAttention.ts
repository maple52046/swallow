import { useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import { INSPECT_HARDWARE_WORKFLOW_KIND, type Operation } from '@/domain/operation/types'

/** How often the attention list is re-read; inspection attempts take minutes, not seconds. */
const INSPECTION_ATTENTION_POLL_MS = 30_000

interface InspectionAttentionScope {
  /** Narrows to one Site; undefined means every Site the operator sees. */
  siteId?: string
  /** Narrows to one Server, for its detail page. */
  serverId?: string
}

/**
 * The inspect-hardware Workflows waiting in `requires_attention` for a Site or one Server
 * (decision 053), so the Server list and detail can say that hardware inspection stopped and how
 * to resume it.
 *
 * Reads the Workflows API (admin scope) on mount, whenever the scope changes, and every 30
 * seconds; the timer and any in-flight response are discarded on unmount or scope change. A failed
 * read yields an empty list rather than an error: the alert is advisory, and the Workflows page
 * remains the authoritative place to see attention.
 */
export function useInspectionAttention({ siteId, serverId }: InspectionAttentionScope): readonly Operation[] {
  const { operations } = useApp()
  const scopeKey = `${siteId ?? ''}|${serverId ?? ''}`
  const [result, setResult] = useState<{ scopeKey: string; items: readonly Operation[] }>({ scopeKey, items: [] })

  useEffect(() => {
    let cancelled = false
    const load = () => {
      operations
        .listOperations({
          siteId, serverId, kind: INSPECT_HARDWARE_WORKFLOW_KIND, status: 'requires_attention', pageSize: 100,
        })
        .then((page) => {
          if (!cancelled) setResult({ scopeKey, items: page.items })
        })
        .catch(() => {
          if (!cancelled) setResult({ scopeKey, items: [] })
        })
    }
    load()
    const timer = window.setInterval(load, INSPECTION_ATTENTION_POLL_MS)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [operations, scopeKey, serverId, siteId])

  // A result of the previous scope is never shown for the new one while its read is in flight.
  return result.scopeKey === scopeKey ? result.items : []
}
