import { HStack } from '@chakra-ui/react'
import type { Platform } from '@/domain/platform/types'
import {
  isPlatformLifecycleInProgress,
  platformChange,
  platformLifecycleLabel,
  platformLifecycleStatus,
} from '@/domain/platform/lifecycle'
import { InProgressSpinner } from '@/presentation/components/InProgressSpinner'
import { StatusBadge } from '@/presentation/components/StatusBadge'

/**
 * The Platform lifecycle state, shared by the Platforms list rows, its mobile cards, and the
 * Platform detail header so the three never drift. While a deployment or uninstall runs, the
 * shared `InProgressSpinner` follows the label, the same cue Server Deployment cells and Workflow
 * status use; it keeps rotating under reduced motion by product request. A change the Platform goes
 * through while it stays usable (adding or removing nodes, reconfiguring) gets its own badge beside
 * the lifecycle, with the spinner while it runs and the word "failed" when it did not succeed. The
 * badge text carries the state, so the spinner is hidden from assistive technology and status never
 * relies on colour or motion alone.
 */
export function PlatformLifecycleBadge({ platform }: { platform: Platform }) {
  const state = platform.lifecycleState
  const change = platformChange(platform)
  return (
    <HStack as="span" gap="1.5" display="inline-flex" wrap="wrap" className="sw-platform-lifecycle-badge">
      <StatusBadge status={platformLifecycleStatus(state)} label={platformLifecycleLabel(state)} />
      {isPlatformLifecycleInProgress(state) && <InProgressSpinner color="blue.fg" />}
      {change && (
        <HStack as="span" gap="1.5" display="inline-flex">
          <StatusBadge status={change.failed ? 'failed' : 'running'} label={change.label} />
          {!change.failed && <InProgressSpinner color="blue.fg" />}
        </HStack>
      )}
    </HStack>
  )
}
