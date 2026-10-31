import { useEffect, useMemo, useState } from 'react'
import { Button, Field, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { GroupingResource } from '@/domain/infrastructure/types'
import type { Server } from '@/domain/server/types'
import { serverDisplayName } from '@/domain/server/list'
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
  zones: GroupingResource[]
  pools: GroupingResource[]
}

type LoadState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; options: PlacementOptions }

/** The select value for "no swallow group": the Server belongs in its provisioner's default group. */
const PROVIDER_DEFAULT = 'provider-default'

/**
 * Sets one Server's Zone and/or Pool in its spec (decision 071).
 *
 * The choices are the groups defined for the Server's own Site plus the provisioner's default
 * group, which clears the placement. The dialog opens on the Server's spec, not on what the
 * provisioner reports, because the spec is what an edit changes; the observed group is shown as a
 * hint. Only changed selections are sent, and submit stays disabled until something changes. The
 * backend moves the machine afterwards, and the Server's provisioner sync status reports when it
 * has.
 */
export function ServerPlacementDialog({ server, onClose, onChanged }: ServerPlacementDialogProps) {
  const { infrastructure } = useApp()
  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const initialZoneId = server.spec.zoneId ?? PROVIDER_DEFAULT
  const initialPoolId = server.spec.poolId ?? PROVIDER_DEFAULT
  const [zoneId, setZoneId] = useState(initialZoneId)
  const [poolId, setPoolId] = useState(initialPoolId)
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const siteId = server.status.source.siteId

  useEffect(() => {
    let cancelled = false
    Promise.all([infrastructure.listGroups('zone', siteId), infrastructure.listGroups('pool', siteId)])
      .then(([zones, pools]) => {
        if (!cancelled) setLoad({ status: 'ready', options: { zones, pools } })
      })
      .catch((caught: Error) => {
        if (!cancelled) setLoad({ status: 'error', message: caught.message })
      })
    return () => {
      cancelled = true
    }
  }, [infrastructure, siteId])

  // Only changed selections are submitted; an unchanged field is omitted so the backend leaves it
  // alone, and the provisioner default is sent as null, which clears that field of the spec.
  const zoneChanged = zoneId !== initialZoneId
  const poolChanged = poolId !== initialPoolId
  const canSubmit = load.status === 'ready' && (zoneChanged || poolChanged) && !submitting

  const emptyForSite = load.status === 'ready' && load.options.zones.length === 0 && load.options.pools.length === 0

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (!canSubmit) return
    setSubmitting(true)
    setError('')
    try {
      // The backend answers with the new spec; the caller reloads the Server, whose conditions
      // report the move, so the result is not needed here.
      const field = (changed: boolean, value: string) =>
        changed ? (value === PROVIDER_DEFAULT ? null : value) : undefined
      await infrastructure.assignServerPlacement(server.id, {
        zoneId: field(zoneChanged, zoneId),
        poolId: field(poolChanged, poolId),
      })
      onChanged()
      onClose()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Placement could not be saved.')
    } finally {
      setSubmitting(false)
    }
  }

  const zoneOptions = useMemo(() => placementOptions(load.status === 'ready' ? load.options.zones : []), [load])
  const poolOptions = useMemo(() => placementOptions(load.status === 'ready' ? load.options.pools : []), [load])

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title="Set zone and pool"
      description={`Place ${serverDisplayName(server)} in a swallow-owned zone and/or pool. The provisioner moves the machine shortly after you save.`}
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
            {error}
          </Alert>
        )}
        {load.status === 'loading' && <Text color="fg.muted">Loading zones and pools…</Text>}
        {load.status === 'error' && (
          <Alert status="error" title="Could not load zones and pools">
            {load.message}
          </Alert>
        )}
        {emptyForSite && (
          <Alert status="info" title="No zones or pools yet">
            Create a zone or pool for this site under Infrastructure to place servers in it.
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
              <Field.HelperText>The provisioner reports {server.status.providerResourcePool || 'no pool'}.</Field.HelperText>
            </Field.Root>
          </>
        )}
      </Stack>
    </Modal>
  )
}

/** The select options for one grouping: the provisioner's default first, then the Site's groups. */
function placementOptions(groups: readonly GroupingResource[]) {
  return [
    { value: PROVIDER_DEFAULT, label: 'Provisioner default' },
    ...groups.map((group) => ({ value: group.id, label: group.spec.name })),
  ]
}
