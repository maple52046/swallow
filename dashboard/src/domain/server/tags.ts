import type { Server } from './types'

/**
 * The manual tags swallow owns for a Server (docs/decisions/071): its `spec.tags` once the backend
 * has adopted the Server. Before adoption (`generation` 0) there is no spec yet, and the observed
 * tags minus the automatic ones stand in for it — the same base the backend applies a tag edit to,
 * so the editor's checkboxes agree with what an edit will produce.
 */
export function serverManualTags(server: Server): string[] {
  if (server.generation > 0) return server.spec.tags
  const automatic = new Set(server.status.automaticTags)
  return server.status.tags.filter((tag) => !automatic.has(tag))
}
