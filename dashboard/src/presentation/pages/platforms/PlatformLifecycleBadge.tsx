import { HStack } from '@chakra-ui/react'
import type { PlatformLifecycleState } from '@/domain/platform/types'
import {
  isPlatformLifecycleInProgress,
  platformLifecycleLabel,
  platformLifecycleStatus,
} from '@/domain/platform/lifecycle'
import { InProgressSpinner } from '@/presentation/components/InProgressSpinner'
import { StatusBadge } from '@/presentation/components/StatusBadge'

/**
 * The Platform lifecycle state, shared by the Platforms list rows, its mobile cards, and the
 * Platform detail header so the three never drift. While a deployment or uninstall runs, the
 * shared `InProgressSpinner` follows the label, the same cue Server Deployment cells and Workflow
 * status use; it keeps rotating under reduced motion by product request. The badge text carries
 * the state, so the spinner is hidden from assistive technology and status never relies on
 * colour or motion alone.
 */
export function PlatformLifecycleBadge({ state }: { state: PlatformLifecycleState }) {
  return (
    <HStack as="span" gap="1.5" display="inline-flex" className="sw-platform-lifecycle-badge">
      <StatusBadge status={platformLifecycleStatus(state)} label={platformLifecycleLabel(state)} />
      {isPlatformLifecycleInProgress(state) && <InProgressSpinner color="blue.fg" />}
    </HStack>
  )
}
