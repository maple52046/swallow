import { useEffect, useMemo, useRef, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { SSHKey } from '@/domain/access/types'
import { isSyncPending } from './sshKeyPresentation'

/** Re-read cadence for a pending key; the backend sync pass usually settles within seconds. */
const POLL_INTERVAL_MS = 2000
/**
 * Reads allowed for one set of pending keys (two minutes at the interval above). A key still
 * pending after that is stalled — for example the API's sync loop is not running — so following
 * stops rather than polling forever; "Sync to provisioners" remains available.
 */
const MAX_POLLS = 60

/**
 * Follows only the SSH Keys whose provisioner status is `pending`, re-reading each through
 * `GET /ssh-keys/{keyId}` and handing the fresh key to `onUpdated`, so the caller can patch just
 * that key in place. The list is never re-read and settled keys are never requested.
 *
 * Lifecycle: following starts when a key becomes pending and continues every POLL_INTERVAL_MS while
 * it stays pending. When the pending set changes (a key settles, or a newly imported key is pending)
 * the timer is replaced and the budget restarts for the new set; a set that is still pending after
 * MAX_POLLS reads is given up on. A failed read is ignored and retried on the next tick (a deleted
 * key's 404 included — it leaves the set once the list no longer contains it). The timer is cleared
 * on unmount, and responses that arrive after cleanup are dropped.
 *
 * Returns the ids currently being followed, so the UI can mark exactly those fields as updating.
 */
export function usePendingSyncRefresh(keys: readonly SSHKey[], onUpdated: (key: SSHKey) => void): ReadonlySet<string> {
  const { sshKeys } = useApp()
  // A stable string identity for the pending set: re-renders that do not change which keys are
  // pending (including this hook's own patches) must not restart the polling loop.
  const pendingKey = useMemo(() => keys.filter(isSyncPending).map((key) => key.id).sort().join(','), [keys])
  const [exhaustedKey, setExhaustedKey] = useState('')
  // The latest callback, read by the polling loop without making it an effect dependency.
  const onUpdatedRef = useRef(onUpdated)
  useEffect(() => {
    onUpdatedRef.current = onUpdated
  }, [onUpdated])

  useEffect(() => {
    if (!pendingKey) return
    const ids = pendingKey.split(',')
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let polls = 0

    const tick = async () => {
      polls += 1
      const results = await Promise.allSettled(ids.map((id) => sshKeys.getSSHKey(id)))
      if (cancelled) return
      for (const result of results) {
        if (result.status === 'fulfilled') onUpdatedRef.current(result.value)
      }
      // A settled key changes pendingKey, which cleans this loop up and starts one for the rest;
      // reaching here with the same set means it is still pending.
      if (polls >= MAX_POLLS) {
        setExhaustedKey(pendingKey)
        return
      }
      timer = setTimeout(() => void tick(), POLL_INTERVAL_MS)
    }

    timer = setTimeout(() => void tick(), POLL_INTERVAL_MS)
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [pendingKey, sshKeys])

  return useMemo(
    () => new Set(pendingKey && pendingKey !== exhaustedKey ? pendingKey.split(',') : []),
    [pendingKey, exhaustedKey],
  )
}
