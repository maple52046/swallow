import { useState } from 'react'
import { Box, Button, Field, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { serverDisplayName, type Server } from '@/domain/server/types'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, failureText } from '@/presentation/components/failure'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { NO_POOL_CHOICE as NO_POOL, usePoolChoices } from '@/presentation/hooks/usePoolChoices'

interface ServerPoolDialogProps {
  /** The selected Servers, or the one Server of a row; each keeps its Zone, only its Pool changes. */
  servers: readonly Server[]
  /** Platform names by id, to name the Platform of a claimed Server; the id stands in for a missing one. */
  platformNames: ReadonlyMap<string, string>
  onClose: () => void
  /** Called after any Server moved, with the ids that did, so the list re-reads and deselects them. */
  onChanged: (movedIds: string[]) => void
}

/** A Server the save could not move, with the API's reason. */
interface PoolMoveFailure {
  server: Server
  message: string
}

/**
 * What saving would do: one Server is named, several are counted against the `selected` total.
 * `movable` are the selected Servers no Platform claims, the only ones a save moves. `poolId` is the
 * select value: empty while nothing is chosen, which happens only when those Servers start in
 * different Pools, or `NO_POOL`.
 */
function poolHelperText(movable: readonly Server[], selected: number, poolId: string, poolLabel: string, moving: number): string {
  if (movable.length === 0) {
    return selected === 1 ? 'This server belongs to a Platform; move the Platform instead.' : 'Every selected server belongs to a Platform; move each Platform instead.'
  }
  if (poolId === '') return 'The selected servers are in different pools.'
  const name = selected === 1 ? serverDisplayName(movable[0]) : null
  if (poolId === NO_POOL) {
    if (name !== null) {
      return moving === 0 ? `${name} is in no pool; only admins may use it.` : `${name} will leave its pool; only admins may use it then.`
    }
    return moving === 0
      ? 'No selected server is in a pool.'
      : `${moving} of ${selected} selected servers will leave their pool; only admins may use them then.`
  }
  if (name !== null) return moving === 0 ? `${name} is already in ${poolLabel}.` : `${name} will move to ${poolLabel}.`
  return moving === 0
    ? `Every selected server is already in ${poolLabel}.`
    : `${moving} of ${selected} selected servers will move to ${poolLabel}.`
}

/**
 * Sets the swallow Pool of the Servers selected in the Server list, or of one Server from its row's Pool
 * shortcut (decision 082). An admin's action:
 * placement is an admin route (`PUT /servers/{id}/placement`), and the caller offers it to admins only.
 *
 * Only `poolId` is sent, so each Server keeps its Zone; a Pool takes effect at once and nothing changes
 * in the provisioner (its resource pool is only observed). A Server a Platform claims changes Pool only
 * with its whole Platform and the API refuses it, so it is never sent: the dialog lists it with its
 * Platform and moves the others. The choice opens on the Pool the movable Servers share, and Servers
 * already in the chosen Pool are skipped. Servers are moved one at a time; one the API refuses (for
 * example one whose spec changed meanwhile) does not stop the others, and the dialog stays open
 * listing each refusal with its reason, while the list re-reads the ones that moved. Dismissal is
 * blocked while the save runs.
 */
export function ServerPoolDialog({ servers, platformNames, onClose, onChanged }: ServerPoolDialogProps) {
  const { infrastructure } = useApp()
  const { showToast } = useToast()
  const { state, options } = usePoolChoices()
  const movable = servers.filter((server) => !server.status.platformClaim)
  const claimed = servers.filter((server) => server.status.platformClaim)
  const shared = new Set(movable.map((server) => server.spec.poolId ?? NO_POOL))
  const [poolId, setPoolId] = useState(shared.size === 1 ? [...shared][0] : '')
  const [saving, setSaving] = useState(false)
  const [failures, setFailures] = useState<PoolMoveFailure[]>([])

  const target = poolId === NO_POOL ? null : poolId
  const moving = poolId === '' ? [] : movable.filter((server) => (server.spec.poolId ?? null) !== target)
  const poolLabel = options.find((option) => option.value === poolId)?.label ?? ''

  const close = () => {
    if (!saving) onClose()
  }

  const save = async () => {
    if (saving || moving.length === 0) return
    setSaving(true)
    setFailures([])
    const moved: string[] = []
    const refused: PoolMoveFailure[] = []
    for (const server of moving) {
      try {
        await infrastructure.assignServerPlacement(server.id, { poolId: target })
        moved.push(server.id)
      } catch (caught) {
        refused.push({ server, message: failureText(failureOf(caught, 'The placement was not saved.')) })
      }
    }
    setSaving(false)
    if (moved.length > 0) onChanged(moved)
    if (refused.length === 0) {
      showToast({ tone: 'success', title: `Pool set for ${moved.length} ${moved.length === 1 ? 'server' : 'servers'}`, description: poolLabel })
      onClose()
      return
    }
    setFailures(refused)
  }

  const count = servers.length
  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!saving}
      title="Set pool"
      description={`Place ${count === 1 ? serverDisplayName(servers[0]) : `${count} servers`} in a pool. Pool members get access at once; each server keeps its zone, and nothing changes in the provisioner.`}
      onSubmit={(event) => {
        event.preventDefault()
        void save()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={saving}>
            {failures.length > 0 ? 'Close' : 'Cancel'}
          </Button>
          <Button type="submit" colorPalette="brand" loading={saving} disabled={saving || moving.length === 0 || state.status !== 'ready'}>
            Set pool
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {state.status === 'loading' && <Text color="fg.muted">Loading pools…</Text>}
        {state.status === 'error' && (
          <Alert status="error" title="Could not load pools">
            <FailureMessage failure={state} />
          </Alert>
        )}
        {state.status === 'ready' && (
          <Field.Root>
            <Field.Label>Pool</Field.Label>
            <Select
              id="server-pool"
              value={poolId}
              aria-label="Pool"
              placeholder="Choose a pool"
              disabled={movable.length === 0}
              onChange={setPoolId}
              options={options}
            />
            <Field.HelperText>
              {poolHelperText(movable, count, poolId, poolLabel, moving.length)}
            </Field.HelperText>
          </Field.Root>
        )}
        {claimed.length > 0 && (
          <Alert status="info" title={`${claimed.length} ${claimed.length === 1 ? 'server keeps its' : 'servers keep their'} Platform's pool`}>
            <Text>A Platform&apos;s servers change pool only together; move the Platform from its page.</Text>
            <Box as="ul" ps="4">
              {claimed.map((server) => {
                const platformId = server.status.platformClaim?.platformId ?? ''
                return <li key={server.id}><strong>{serverDisplayName(server)}</strong>: Platform {platformNames.get(platformId) ?? platformId}</li>
              })}
            </Box>
          </Alert>
        )}
        {failures.length > 0 && (
          <Alert status="warning" title={`${failures.length} ${failures.length === 1 ? 'server was' : 'servers were'} not moved`}>
            <Box as="ul" ps="4">
              {failures.map(({ server, message }) => (
                <li key={server.id}><strong>{serverDisplayName(server)}</strong>: {message}</li>
              ))}
            </Box>
          </Alert>
        )}
      </Stack>
    </Modal>
  )
}
