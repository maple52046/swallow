import type { ServerRepository } from '@/application/ports/ServerRepository'

const MAX_CONCURRENT_REFRESHES = 4

/**
 * Advances selected provisioning projections from their live providers with bounded
 * concurrency. Individual refresh errors do not stop the remaining targets: a later
 * poll can recover a transient provider failure while every accepted Release continues
 * to be observed.
 */
export async function refreshServerProjections(
  repository: ServerRepository,
  serverIds: readonly string[],
): Promise<void> {
  const queue = [...new Set(serverIds)]
  let cursor = 0

  async function worker(): Promise<void> {
    while (cursor < queue.length) {
      const index = cursor
      cursor += 1
      try {
        await repository.refreshServer(queue[index])
      } catch {
        // Polling is best effort. The next attempt may recover, and the bounded loop
        // still stops after its normal timeout when a provider remains unavailable.
      }
    }
  }

  await Promise.all(
    Array.from(
      { length: Math.min(MAX_CONCURRENT_REFRESHES, queue.length) },
      () => worker(),
    ),
  )
}
