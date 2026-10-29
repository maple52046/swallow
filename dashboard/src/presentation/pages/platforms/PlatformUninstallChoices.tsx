import { BlockingWorkflowsNotice } from '@/presentation/components/BlockingWorkflowsNotice'
import { ReleaseOptionsFields } from '@/presentation/components/ReleaseOptionsFields'
import type { ReleaseOptionsValue } from '@/presentation/components/releaseOptions'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import type { ActiveOperationSummary } from '@/presentation/hooks/useServerActiveOperations'
import { platformBlockingWorkflowsLabel } from './platformRemovalCopy'

interface PlatformUninstallChoicesProps {
  /** `single` for one Platform's dialog, `bulk` for a selection; only the wording differs. */
  scope: 'single' | 'bulk'
  /** Prefix for control ids, unique per dialog (`uninstall` or `bulk-uninstall`). */
  idPrefix: string
  releaseServers: boolean
  onReleaseServersChange: (checked: boolean) => void
  releaseOptions: ReleaseOptionsValue
  onReleaseOptionsChange: (value: ReleaseOptionsValue) => void
  /** Unfinished Workflows holding the targets; the consent notice renders only when non-empty. */
  blockingWorkflows: readonly ActiveOperationSummary[]
  cancelBlocking: boolean
  onCancelBlockingChange: (checked: boolean) => void
  /** Disables every control while the request is in flight. */
  disabled?: boolean
}

/**
 * The host-side options an Uninstall can carry, shared by the single-Platform and bulk Uninstall
 * dialogs so both offer them identically: release the servers instead of uninstalling the software
 * (with the shared release options), and the consent for the backend to cancel the Workflows still
 * holding the servers (`cancelActiveWorkflows`, decision 061). Whether the record is deleted too is
 * not chosen here but on submit, through the variant behind the submit button's chevron.
 *
 * The caller owns the state and resets the typed confirmation and cancel consent when the release
 * choice changes, because it changes what the operator is agreeing to.
 */
export function PlatformUninstallChoices({
  scope,
  idPrefix,
  releaseServers,
  onReleaseServersChange,
  releaseOptions,
  onReleaseOptionsChange,
  blockingWorkflows,
  cancelBlocking,
  onCancelBlockingChange,
  disabled,
}: PlatformUninstallChoicesProps) {
  const bulk = scope === 'bulk'
  return (
    <>
      <Checkbox id={`${idPrefix}-release-servers`} checked={releaseServers} disabled={disabled} onCheckedChange={onReleaseServersChange}>
        Release all servers instead of uninstalling platform software
      </Checkbox>
      {releaseServers && (
        <ReleaseOptionsFields idPrefix={`${idPrefix}-release`} value={releaseOptions} onChange={onReleaseOptionsChange} />
      )}
      <BlockingWorkflowsNotice
        operations={blockingWorkflows}
        title={bulk
          ? blockingWorkflows.length === 1
            ? 'An unfinished workflow still holds these platforms’ servers'
            : `${blockingWorkflows.length} unfinished workflows still hold these platforms’ servers`
          : blockingWorkflows.length === 1
            ? 'An unfinished workflow still holds these servers'
            : `${blockingWorkflows.length} unfinished workflows still hold these servers`}
        checkboxId={`${idPrefix}-cancel-blocking`}
        checkboxLabel={platformBlockingWorkflowsLabel(releaseServers, scope)}
        listLabel={bulk ? "Workflows holding the selected platforms' servers" : "Workflows holding the platform's servers"}
        checked={cancelBlocking}
        disabled={disabled}
        onCheckedChange={onCancelBlockingChange}
      >
        {bulk
          ? 'An uninstall is refused while another workflow holds its servers. Check the box to cancel them: each uninstall is accepted now and starts by itself once the canceled workflows have ended. A canceled OS deployment first returns the servers it was installing to Ready, which can take a few minutes; finished deployments are kept.'
          : 'The uninstall is refused while another workflow holds the servers. Check the box to cancel it: the uninstall is accepted now and starts by itself once the canceled workflow has ended. A canceled OS deployment first returns the servers it was installing to Ready, which can take a few minutes; finished deployments are kept.'}
      </BlockingWorkflowsNotice>
    </>
  )
}
