import type { ServerRepository } from '@/application/ports/ServerRepository'

const MAX_CONCURRENT_DELETES = 4

/** One Server's deletion result: no error means the Machine and the Swallow record are gone. */
export interface ServerDeleteOutcome {
  serverId: string
  error?: unknown
}

/**
 * Deletes several Servers through the per-Server provider-first API (server-detail-actions.md):
 * Swallow has no bulk delete endpoint, and each deletion is synchronous because it waits for the
 * provisioner. At most four run at once so a large selection does not flood the provisioner, and
 * one refusal never stops the others. `onSettled` reports progress as each deletion finishes.
 * Outcomes keep the input order so callers can pair them with their targets.
 */
export async function deleteServers(
  repository: ServerRepository,
  serverIds: readonly string[],
  onSettled?: (settled: number) => void,
): Promise<ServerDeleteOutcome[]> {
  const outcomes: ServerDeleteOutcome[] = serverIds.map((serverId) => ({ serverId }))
  let cursor = 0
  let settled = 0

  async function worker(): Promise<void> {
    while (cursor < serverIds.length) {
      const index = cursor
      cursor += 1
      try {
        await repository.deleteServer(serverIds[index])
      } catch (error) {
        outcomes[index].error = error
      }
      settled += 1
      onSettled?.(settled)
    }
  }

  await Promise.all(
    Array.from({ length: Math.min(MAX_CONCURRENT_DELETES, serverIds.length) }, () => worker()),
  )
  return outcomes
}
