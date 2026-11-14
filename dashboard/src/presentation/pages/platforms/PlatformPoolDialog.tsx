import { useState } from 'react'
import { Button, Field, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { PlatformPoolMove } from '@/domain/infrastructure/types'
import type { Platform } from '@/domain/platform/types'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { NO_POOL_CHOICE, usePoolChoices } from '@/presentation/hooks/usePoolChoices'

interface PlatformPoolDialogProps {
  platform: Platform
  onClose: () => void
  /** Called after the move with its result, so the page re-reads the Platform and its Pool. */
  onMoved: (result: PlatformPoolMove) => void
}

/**
 * What saving would do. `poolId` is the select value: empty while nothing is chosen, which happens
 * only when the Platform has no Pool (its Servers are in none, or in different ones), or
 * {@link NO_POOL_CHOICE}. `unchanged` is a choice that names the Pool the Platform is already in.
 */
function moveHelperText(platformName: string, poolId: string, poolLabel: string, unchanged: boolean): string {
  if (poolId === '') return 'Choose the pool for all of its servers.'
  if (unchanged) return `${platformName} is already in ${poolLabel}.`
  if (poolId === NO_POOL_CHOICE) return `${platformName} and all its servers will leave their pool; only admins may use them then.`
  return `${platformName} and all its servers will move to ${poolLabel}.`
}

/**
 * Moves a Platform to another Pool with every one of its Servers (platforms.md, Move A Platform To
 * Another Pool), the only way a Server a Platform claims changes Pool. It is an admin's action: the
 * route is admin-only, and the caller offers the dialog to admins only.
 *
 * The choice opens on the Platform's Pool, or on nothing when it has none (its Servers are in no Pool
 * or across Pools, which the API reports the same way); choosing the Pool it is already in disables
 * the save. The API moves all the Servers in one write, keeping their Zones, and refuses the whole move
 * on a conflict (a Server changed meanwhile or not adopted yet), which the dialog shows with its
 * request ID while it stays open. Access follows at once; the hosts, the Platform's spec and
 * Workflows, and the provisioner are not changed, which the description says before the user saves.
 * Dismissal is blocked while the save runs.
 */
export function PlatformPoolDialog({ platform, onClose, onMoved }: PlatformPoolDialogProps) {
  const { infrastructure } = useApp()
  const { showToast } = useToast()
  const { state, options } = usePoolChoices()
  const current = platform.status.poolId ?? null
  const [poolId, setPoolId] = useState(current ?? '')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)

  const target = poolId === NO_POOL_CHOICE ? null : poolId
  const unchanged = current !== null && target === current
  const poolLabel = options.find((option) => option.value === poolId)?.label ?? ''

  const close = () => {
    if (!saving) onClose()
  }

  const save = async () => {
    if (saving || poolId === '' || unchanged) return
    setSaving(true)
    setError(null)
    try {
      const result = await infrastructure.movePlatformToPool(platform.id, target)
      const count = result.servers.length
      showToast({
        tone: 'success',
        title: target === null ? `${platform.name} left its pool` : `Moved ${platform.name} to ${poolLabel}`,
        description: `${count} ${count === 1 ? 'server' : 'servers'} moved with it.`,
      })
      setSaving(false)
      onMoved(result)
      onClose()
    } catch (caught) {
      setError(failureOf(caught, 'The platform was not moved.'))
      setSaving(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!saving}
      title="Move to another pool"
      description={`Every server of ${platform.name} moves to the chosen pool together. The pool's owners manage the platform and its members can log in to its servers at once; each server keeps its zone, and nothing changes on the hosts, in the platform, or in the provisioner.`}
      onSubmit={(event) => {
        event.preventDefault()
        void save()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={saving}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={saving} disabled={saving || poolId === '' || unchanged || state.status !== 'ready'}>
            Move platform
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="The platform was not moved">
            <FailureMessage failure={error} />
          </Alert>
        )}
        {state.status === 'loading' && <Text color="fg.muted">Loading pools…</Text>}
        {state.status === 'error' && (
          <Alert status="error" title="Could not load pools">
            <FailureMessage failure={state} />
          </Alert>
        )}
        {state.status === 'ready' && (
          <Field.Root>
            <Field.Label>Pool</Field.Label>
            <Select id="platform-pool" value={poolId} aria-label="Pool" placeholder="Choose a pool" onChange={setPoolId} options={options} />
            <Field.HelperText>{moveHelperText(platform.name, poolId, poolLabel, unchanged)}</Field.HelperText>
          </Field.Root>
        )}
      </Stack>
    </Modal>
  )
}
