import { useEffect, useState } from 'react'
import type { ServerRepository } from '@/application/ports/ServerRepository'
import type { ServerBootMedia } from '@/domain/server/types'

/** How often a running preflight's progress is re-read; it changes phase every few seconds to minutes. */
const POLL_MS = 2_000

/**
 * Follows a Boot Media enable preflight on one Server (contract `apply`) for the Server Summary.
 *
 * `base` is the page's last full read of Boot Media. While a preflight runs — the dialog said it
 * sent one (`start`), or the read shows one, for example after a reload or from another tab —
 * this re-reads Boot Media every two seconds and returns the newest read as `media`; it stops
 * when the read shows none and nothing is being watched. A newer `base` (the page re-read after
 * the request ended) always wins over an older poll, so a late answer cannot bring back finished
 * progress. A failed poll keeps the last answer; the next tick retries. The interval stops on
 * unmount.
 */
export function useBootMediaApplyWatch(servers: ServerRepository, serverId: string, base: ServerBootMedia | null) {
  const [watching, setWatching] = useState(false)
  const [polled, setPolled] = useState<{ base: ServerBootMedia; media: ServerBootMedia } | null>(null)
  const media = polled && polled.base === base ? polled.media : base
  const running = base !== null && (watching || Boolean(media?.apply))

  useEffect(() => {
    if (!running || !base) return
    let cancelled = false
    const timer = window.setInterval(() => {
      servers
        .getBootMedia(serverId)
        .then((next) => {
          if (!cancelled) setPolled({ base, media: next })
        })
        .catch(() => {
          // A missed poll only delays the progress; keep the last answer until the next tick.
        })
    }, POLL_MS)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [running, base, servers, serverId])

  return {
    media,
    /** Whether a preflight is running or was just sent from this page. */
    applying: watching || Boolean(media?.apply),
    start: () => setWatching(true),
    stop: () => setWatching(false),
  }
}
