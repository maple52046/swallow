import { serverDisplayName, type Server } from '@/domain/server/types'

/**
 * A deploy review value naming the Servers that take one role, so a user checks who does what
 * before submitting, not only how many: their display names in the order the request names them,
 * joined with commas, or "None" when no Server takes the role. An id missing from `servers` keeps
 * its id, so the review never drops a member. `note` adds a parenthesised qualifier to one Server,
 * for example a Kubernetes control plane that also runs workloads or the Slurm primary controller.
 */
export function deployReviewServerNames(
  servers: readonly Server[],
  serverIds: readonly string[],
  note?: (serverId: string, index: number) => string | undefined,
): string {
  if (serverIds.length === 0) return 'None'
  return serverIds.map((serverId, index) => {
    const server = servers.find((candidate) => candidate.id === serverId)
    const name = server ? serverDisplayName(server) : serverId
    const qualifier = note?.(serverId, index)
    return qualifier ? `${name} (${qualifier})` : name
  }).join(', ')
}
