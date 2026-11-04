import { useState } from 'react'
import { Button, Field, Input, List, Stack, Text } from '@chakra-ui/react'
import { platformDeploymentServerIds } from '@/domain/platform/lifecycle'
import { emptyReleaseOptions, type ReleaseOptionsValue } from '@/presentation/components/releaseOptions'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { useServerActiveOperations } from '@/presentation/hooks/useServerActiveOperations'
import { plural } from '@/shared/utils/plural'
import { PlatformBulkFailures, PlatformNameList } from './PlatformBulkLists'
import { PlatformUninstallChoices } from './PlatformUninstallChoices'
import {
  platformBulkUninstallConfirmLabel,
  platformBulkUninstallDialogCopy,
  platformUninstallConsequence,
  platformUninstallDeleteHint,
} from './platformRemovalCopy'
import { platformUninstallOptions } from './platformUninstallRequest'
import { usePlatformBulkActions, type PlatformBulkOutcome } from './usePlatformBulkActions'
import type { PlatformUninstallEligibility } from './usePlatformUninstallEligibility'

interface PlatformBulkUninstallDialogProps {
  /** The selection split by `usePlatformUninstallEligibility`; only `eligible` is submitted. */
  eligibility: PlatformUninstallEligibility
  onClose: () => void
  /**
   * Called with the accepted outcomes once at least one request was accepted. Each carries the
   * Change Plan it proposed, if any, so the page can follow the plans to Change Plans, or else
   * reload and keep only skipped and failed Platforms selected for a follow-up action.
   */
  onDone: (accepted: readonly PlatformBulkOutcome[]) => void
}

/**
 * Confirms and fans out Uninstall across the eligible Platforms of a bulk selection, listing the
 * skipped ones with their reason (not deployed by swallow, already uninstalling or uninstalled, a
 * locked or unreadable target). It offers the same choices as the single dialog: release the
 * servers instead, and, through the variant behind the submit chevron, delete each record after its
 * own operation succeeds (`deleteRecord`). With the release choice checked the title, intro, and
 * target list describe a release of the servers, not a software uninstall.
 *
 * Bulk destructive actions cannot ask a user to type every platform name, so the gate is
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

  const submit = async () => {
    if (!canSubmit) return
    setFailures([])
    const outcomes = await runUninstall(eligible, platformUninstallOptions({
      releaseServers, releaseOptions, cancelActiveWorkflows: blocked && cancelBlocking,
    }))
    const accepted = outcomes.filter((outcome) => outcome.accepted)
    if (accepted.length === 0) {
      setFailures(outcomes)
      return
    }
    onDone(accepted)
  }

  const consequence = platformUninstallConsequence(releaseServers, 'bulk')
  const dialogCopy = platformBulkUninstallDialogCopy({ releaseServers })

  return (
    <Modal
      open
      onClose={close}
      size="lg"
      closeOnInteractOutside={!running}
      dismissDisabled={running}
      title={dialogCopy.title}
      description={dialogCopy.description}
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={running}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="red" loading={running} disabled={!canSubmit}>
            {platformBulkUninstallConfirmLabel({ releaseServers }, count)}
          </Button>
        </>
      }
    >
      <Stack gap="4">
        <PlatformBulkFailures failures={failures} />
        {/* Polite so a screen reader hears the new consequence when a choice changes. */}
        <Stack gap="3" aria-live="polite">
          <PlatformNameList label={dialogCopy.listLabel} platforms={eligible} />
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
        <Text fontSize="sm" color="fg.muted">{platformUninstallDeleteHint(releaseServers, 'bulk')}</Text>
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
