import type { DockerContainerState } from '@/domain/software/docker'

/**
 * Props every Docker Host Explorer section receives from the Containers tab.
 *
 * - `serverId` addresses the Server; sections never see a host address (api-server dials it).
 * - `readOnlyReason` is set while the Server is locked: write controls render disabled and the tab
 *   shows the reason once. The server still refuses writes on its own; this only avoids offering
 *   actions that would fail.
 * - `onChanged` lets a section tell the explorer a write succeeded, so the summary counts refresh.
 */
export interface DockerSectionProps {
  serverId: string
  readOnlyReason?: string
  onChanged: () => void
}

/**
 * Maps a Docker Engine container state to a `StatusBadge` colour family. The badge always shows
 * the Engine's own state text, so colour only reinforces it; an unrecognised future state falls
 * back to the neutral family instead of reading as a failure.
 */
export function containerStateBadgeStatus(state: DockerContainerState): string {
  switch (state) {
    case 'running':
      return 'healthy'
    case 'restarting':
      return 'pending'
    case 'paused':
    case 'removing':
      return 'warning'
    case 'dead':
      return 'failed'
    case 'created':
    case 'exited':
      return 'offline'
    default:
      return 'unknown'
  }
}

/** Whether Start is meaningful: anything not already running or being torn down. */
export function canStartContainer(state: DockerContainerState): boolean {
  return state === 'created' || state === 'exited' || state === 'dead'
}

/** Whether Stop and Restart are meaningful: the container has a live process. */
export function canStopContainer(state: DockerContainerState): boolean {
  return state === 'running' || state === 'restarting' || state === 'paused'
}

/** Read-only explanation shown (once) and applied to every write control while the Server is locked. */
export const LOCKED_DOCKER_READ_ONLY_REASON =
  'This Server is locked, so its Docker objects are read-only. Unlock the Server to pull, create, start, stop, or remove them.'

/** Message appended to an explorer eligibility notice (a 409 from the contract). */
export const DOCKER_EXPLORER_UNAVAILABLE_HINT =
  'The Docker explorer works once swallow has installed Docker CE on this Server with the Docker Engine API enabled.'
