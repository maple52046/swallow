import { useEffect, useMemo, useState } from 'react'
import { Button, Field, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { Pool, Zone } from '@/domain/infrastructure/types'
import type { Server } from '@/domain/server/types'
import { serverDisplayName } from '@/domain/server/types'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'

interface ServerPlacementDialogProps {
  server: Server
  onClose: () => void
  /** Called after a successful placement so the caller can reload the projection. */
  onChanged: () => void
}

interface PlacementOptions {
  zones: Zone[]
  pools: Pool[]
}

type LoadState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; options: PlacementOptions }

/**
 * The select value for "no swallow group": the provisioner's default zone for the Zone field, and
 * no Pool (admins only) for the Pool field. Either is sent as `null`.
 */
const NO_GROUP = 'no-group'

/**
 * Sets one Server's Zone and/or Pool in its spec (decisions 071 and 082).
 *
 * The Zone choices are the Zones of the Server's own Site plus the provisioner's default zone; the
 * Pool choices are every Pool of the installation, because a Pool may hold Servers of any Site, plus
 * "No pool", which leaves the Server to admins. The dialog opens on the Server's spec, not on what
 * the provisioner reports, because the spec is what an edit changes; the observed zone is shown as a
 * hint. Only changed selections are sent, and submit stays disabled until something changes. The
 * backend moves the machine to its Zone afterwards, and the Server's provisioner sync status reports
 * when it has; a Pool takes effect at once and never touches the provisioner.
 */
export function ServerPlacementDialog({ server, onClose, onChanged }: ServerPlacementDialogProps) {
  const { infrastructure } = useApp()
  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const initialZoneId = server.spec.zoneId ?? NO_GROUP
  const initialPoolId = server.spec.poolId ?? NO_GROUP
  const [zoneId, setZoneId] = useState(initialZoneId)
  const [poolId, setPoolId] = useState(initialPoolId)
  const [error, setError] = useState<DisplayedFailure | null>(null)
  const [submitting, setSubmitting] = useState(false)

  const siteId = server.status.source.siteId

  // Reads the choices once per Server Site; the flag drops a late answer after the dialog closed.
  useEffect(() => {
    let cancelled = false
    Promise.all([infrastructure.listZones(siteId), infrastructure.listPools()])
      .then(([zones, pools]) => {
        if (!cancelled) setLoad({ status: 'ready', options: { zones, pools } })
      })
      .catch((caught: Error) => {
        if (!cancelled) setLoad({ status: 'error', ...failureOf(caught, 'Zones and pools could not be loaded.') })
      })
    return () => {
      cancelled = true
    }
  }, [infrastructure, siteId])

  // Only changed selections are submitted; an unchanged field is omitted so the backend leaves it
  // alone, and "no group" is sent as null, which clears that field of the spec.
  const zoneChanged = zoneId !== initialZoneId
  const poolChanged = poolId !== initialPoolId
  const canSubmit = load.status === 'ready' && (zoneChanged || poolChanged) && !submitting

  const nothingToChoose = load.status === 'ready' && load.options.zones.length === 0 && load.options.pools.length === 0

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (!canSubmit) return
    setSubmitting(true)
    setError(null)
    try {
      // The backend answers with the new spec; the caller reloads the Server, whose conditions
      // report the move, so the result is not needed here.
      const field = (changed: boolean, value: string) =>
        changed ? (value === NO_GROUP ? null : value) : undefined
      await infrastructure.assignServerPlacement(server.id, {
        zoneId: field(zoneChanged, zoneId),
        poolId: field(poolChanged, poolId),
      })
      onChanged()
      onClose()
    } catch (caught) {
      setError(failureOf(caught, 'Placement could not be saved.'))
    } finally {
      setSubmitting(false)
    }
  }

  const zoneOptions = useMemo(
    () => [
      { value: NO_GROUP, label: 'Provisioner default' },
      ...(load.status === 'ready' ? load.options.zones : []).map((zone) => ({ value: zone.id, label: zone.spec.name })),
    ],
    [load],
  )
  const poolOptions = useMemo(
    () => [
      { value: NO_GROUP, label: 'No pool (admins only)' },
      ...(load.status === 'ready' ? load.options.pools : []).map((pool) => ({ value: pool.id, label: pool.name })),
    ],
    [load],
  )

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title="Set zone and pool"
      description={`Place ${serverDisplayName(server)} in a zone of its site and in a pool. The provisioner moves the machine to the zone shortly after you save; the pool decides who may use it.`}
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={!canSubmit}>
            Save placement
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Placement could not be saved">
            <FailureMessage failure={error} />
          </Alert>
        )}
        {load.status === 'loading' && <Text color="fg.muted">Loading zones and pools…</Text>}
        {load.status === 'error' && (
          <Alert status="error" title="Could not load zones and pools">
            <FailureMessage failure={load} />
          </Alert>
        )}
        {nothingToChoose && (
          <Alert status="info" title="No zones or pools yet">
            Create a zone for this site under Infrastructure, or a pool on the Pools page, to place servers in it.
          </Alert>
        )}
        {load.status === 'ready' && (
          <>
            <Field.Root>
              <Field.Label>Zone</Field.Label>
              <Select
                id="placement-zone"
                value={zoneId}
                aria-label="Zone"
                onChange={setZoneId}
                options={zoneOptions}
              />
              <Field.HelperText>The provisioner reports {server.status.providerZone || 'no zone'}.</Field.HelperText>
            </Field.Root>
            <Field.Root>
              <Field.Label>Pool</Field.Label>
              <Select
                id="placement-pool"
                value={poolId}
                aria-label="Pool"
                onChange={setPoolId}
                options={poolOptions}
              />
              <Field.HelperText>Pool members get access to this server in any site; nothing changes in the provisioner.</Field.HelperText>
            </Field.Root>
          </>
        )}
      </Stack>
    </Modal>
  )
}
