import { useState } from 'react'
import { Button, Field, Input, List, Stack, Text } from '@chakra-ui/react'
import { platformDeploymentServerIds } from '@/domain/platform/lifecycle'
import { emptyReleaseOptions, type ReleaseOptionsValue } from '@/presentation/components/releaseOptions'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { useServerActiveOperations } from '@/presentation/hooks/useServerActiveOperations'
import { PlatformBulkFailures, PlatformNameList } from './PlatformBulkLists'
import { PlatformRemovalSplitButton } from './PlatformRemovalSplitButton'
import { PlatformUninstallChoices } from './PlatformUninstallChoices'
import {
  platformBulkUninstallConfirmLabel,
  platformUninstallConsequence,
  platformUninstallDeleteHint,
  plural,
} from './platformRemovalCopy'
import { platformUninstallOptions } from './platformUninstallRequest'
import { usePlatformBulkActions, type PlatformBulkOutcome } from './usePlatformBulkActions'
import type { PlatformUninstallEligibility } from './usePlatformUninstallEligibility'

interface PlatformBulkUninstallDialogProps {
  /** The selection split by `usePlatformUninstallEligibility`; only `eligible` is submitted. */
  eligibility: PlatformUninstallEligibility
  onClose: () => void
  /**
   * Called with the ids whose request was accepted once at least one was, so the page reloads and
   * keeps only skipped and failed Platforms selected for a follow-up action.
   */
  onDone: (acceptedIds: readonly string[]) => void
}

/**
 * Confirms and fans out Uninstall across the eligible Platforms of a bulk selection, listing the
 * skipped ones with their reason (not deployed by Swallow, already uninstalling or uninstalled, a
 * locked or unreadable target). It offers the same choices as the single dialog: release the
 * servers instead, and, through the variant behind the submit chevron, delete each record after its
 * own operation succeeds (`deleteRecord`).
 *
 * Bulk destructive actions cannot ask an operator to type every platform name, so the gate is
 * typing `uninstall`; the record choice is then the explicit submit variant. The dialog lists the
 * unfinished Workflows holding any targeted platform's servers and requires confirming that the
 * backend cancels them first (`cancelActiveWorkflows`, decision 061). It stays open with each
 * failure listed when every request failed; otherwise it hands control back to the page.
 */
export function PlatformBulkUninstallDialog({ eligibility, onClose, onDone }: PlatformBulkUninstallDialogProps) {
  const { runUninstall, running } = usePlatformBulkActions()
  const { eligible, skipped } = eligibility
  const [confirmation, setConfirmation] = useState('')
  const [failures, setFailures] = useState<PlatformBulkOutcome[]>([])
  const [releaseServers, setReleaseServers] = useState(false)
  const [releaseOptions, setReleaseOptions] = useState<ReleaseOptionsValue>(emptyReleaseOptions)
  const [cancelBlocking, setCancelBlocking] = useState(false)
  const verb = 'uninstall'
  const count = eligible.length

  const { activeOperations: blockingWorkflows } = useServerActiveOperations([...new Set(eligible.flatMap(platformDeploymentServerIds))])
  const blocked = blockingWorkflows.length > 0
  const canSubmit = confirmation === verb && !running && count > 0 && !(blocked && !cancelBlocking)

  const close = () => {
    if (!running) onClose()
  }

  // Releasing changes what the typed verb and the cancel consent agree to, so both are asked for
  // again.
  const changeReleaseServers = (checked: boolean) => {
    setReleaseServers(checked)
    if (!checked) setReleaseOptions(emptyReleaseOptions)
    setConfirmation('')
    setCancelBlocking(false)
  }

  // `deleteRecord` is chosen at submit time: the primary part keeps the records, and the variant
  // in its menu also deletes each one after its own operation succeeds.
  const submit = async (deleteRecord = false) => {
    if (!canSubmit) return
    setFailures([])
    const outcomes = await runUninstall(eligible, platformUninstallOptions({
      releaseServers, releaseOptions, deleteRecord, cancelActiveWorkflows: blocked && cancelBlocking,
    }))
    const accepted = outcomes.filter((outcome) => outcome.accepted)
    if (accepted.length === 0) {
      setFailures(outcomes)
      return
    }
    onDone(accepted.map((outcome) => outcome.platformId))
  }

  const consequence = platformUninstallConsequence(releaseServers, 'bulk')

  return (
    <Modal
      open
      onClose={close}
      size="lg"
      closeOnInteractOutside={!running}
      dismissDisabled={running}
      title="Uninstall selected platforms"
      description="Uninstall platform software from selected Platforms that are currently eligible. Ineligible selections will be skipped."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={running}>
            Cancel
          </Button>
          <PlatformRemovalSplitButton
            primaryType="submit"
            primaryLabel={platformBulkUninstallConfirmLabel({ releaseServers, deleteRecord: false }, count)}
            disabled={!canSubmit}
            loading={running}
            menuItemLabel={platformBulkUninstallConfirmLabel({ releaseServers, deleteRecord: true }, count)}
            onMenuSelect={() => void submit(true)}
            menuTriggerLabel="More uninstall options"
            placement="dialog"
          />
        </>
      }
    >
      <Stack gap="4">
        <PlatformBulkFailures failures={failures} />
        {/* Polite so a screen reader hears the new consequence when a choice changes. */}
        <Stack gap="3" aria-live="polite">
          <PlatformNameList label="Platforms to uninstall" platforms={eligible} />
          <Alert status={consequence.status} title={consequence.title}>
            {consequence.body}
          </Alert>
        </Stack>
        {skipped.length > 0 && (
          <Alert status="info" title={`${skipped.length} selected ${plural(skipped.length, 'platform')} skipped`}>
            <Stack gap="1">
              <Text>These selected platforms cannot be uninstalled and will remain unchanged by this action.</Text>
              <List.Root aria-label="Platforms skipped by uninstall" className="sw-dialog-name-list" ps="4">
                {skipped.map((entry) => (
                  <List.Item key={entry.platform.id}>{entry.platform.name} — {entry.reason}</List.Item>
                ))}
              </List.Root>
            </Stack>
          </Alert>
        )}
        <PlatformUninstallChoices
          scope="bulk"
          idPrefix="bulk-uninstall"
          releaseServers={releaseServers}
          onReleaseServersChange={changeReleaseServers}
          releaseOptions={releaseOptions}
          onReleaseOptionsChange={setReleaseOptions}
          blockingWorkflows={blockingWorkflows}
          cancelBlocking={cancelBlocking}
          onCancelBlockingChange={setCancelBlocking}
          disabled={running}
        />
        <Text fontSize="sm" color="fg.muted">{platformUninstallDeleteHint(releaseServers, 'bulk', count)}</Text>
        <Field.Root required>
          <Field.Label>
            Type "{verb}" to confirm <Field.RequiredIndicator />
          </Field.Label>
          <Input value={confirmation} disabled={running} onChange={(event) => setConfirmation(event.target.value)} autoFocus aria-label={`Type ${verb} to confirm`} />
        </Field.Root>
      </Stack>
    </Modal>
  )
}
