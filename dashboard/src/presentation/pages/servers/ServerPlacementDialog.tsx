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

/**
 * Assigns one Server to a swallow-owned Zone and/or Pool (decision 029).
 *
 * The choices are the groups defined for the Server's own Site; the current selections are
 * pre-matched from the Server's observed provider zone/pool by name. Only changed selections are
 * sent, and submit stays disabled until something changes, so the request never re-asserts the
 * current placement. The provisioner is written through, so the observed value on the projection
 * converges on the next reconcile; this dialog reports the effective names immediately.
 */
export function ServerPlacementDialog({ server, onClose, onChanged }: ServerPlacementDialogProps) {
  const { infrastructure } = useApp()
  const [load, setLoad] = useState<LoadState>({ status: 'loading' })
  const [zoneId, setZoneId] = useState('')
  const [poolId, setPoolId] = useState('')
  const [initialZoneId, setInitialZoneId] = useState('')
  const [initialPoolId, setInitialPoolId] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const siteId = server.source.siteId

  useEffect(() => {
    let cancelled = false
    Promise.all([infrastructure.listGroups('zone', siteId), infrastructure.listGroups('pool', siteId)])
      .then(([zones, pools]) => {
        if (cancelled) return
        // Pre-match the current selection from the observed provider names so the dialog opens on
        // the Server's real placement rather than blank.
        const currentZone = zones.find((zone) => zone.name === server.providerZone)?.id ?? ''
        const currentPool = pools.find((pool) => pool.name === server.providerResourcePool)?.id ?? ''
        setZoneId(currentZone)
        setPoolId(currentPool)
        setInitialZoneId(currentZone)
        setInitialPoolId(currentPool)
        setLoad({ status: 'ready', options: { zones, pools } })
      })
      .catch((caught: Error) => {
        if (!cancelled) setLoad({ status: 'error', message: caught.message })
      })
    return () => {
      cancelled = true
    }
  }, [infrastructure, siteId, server.providerZone, server.providerResourcePool])

  // Only changed selections are submitted; an unchanged field is omitted so the backend leaves it
  // alone and the request always carries a real change.
  const zoneChanged = zoneId !== '' && zoneId !== initialZoneId
  const poolChanged = poolId !== '' && poolId !== initialPoolId
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
      // The backend returns the effective zone/pool names; the caller reports them and the
      // projection converges on the next reconcile, so the result is not needed here.
      await infrastructure.assignServerPlacement(server.id, {
        zoneId: zoneChanged ? zoneId : undefined,
        poolId: poolChanged ? poolId : undefined,
      })
      onChanged()
      onClose()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Placement could not be saved.')
    } finally {
      setSubmitting(false)
    }
  }

  const zoneOptions = useMemo(
    () => (load.status === 'ready' ? load.options.zones.map((zone) => ({ value: zone.id, label: zone.name })) : []),
    [load],
  )
  const poolOptions = useMemo(
    () => (load.status === 'ready' ? load.options.pools.map((pool) => ({ value: pool.id, label: pool.name })) : []),
    [load],
  )

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title="Set zone and pool"
      description={`Assign ${serverDisplayName(server)} to a swallow-owned zone and/or pool. The provisioner is updated when it supports grouping.`}
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
            Create a zone or pool for this site under Infrastructure before assigning servers.
          </Alert>
        )}
        {load.status === 'ready' && !emptyForSite && (
          <>
            <Field.Root>
              <Field.Label>Zone</Field.Label>
              <Select
                id="placement-zone"
                value={zoneId}
                aria-label="Zone"
                placeholder={zoneOptions.length ? 'Select a zone' : 'No zones defined'}
                disabled={zoneOptions.length === 0}
                onChange={setZoneId}
                options={zoneOptions}
              />
              <Field.HelperText>Currently {server.providerZone || 'unset'}.</Field.HelperText>
            </Field.Root>
            <Field.Root>
              <Field.Label>Pool</Field.Label>
              <Select
                id="placement-pool"
                value={poolId}
                aria-label="Pool"
                placeholder={poolOptions.length ? 'Select a pool' : 'No pools defined'}
                disabled={poolOptions.length === 0}
                onChange={setPoolId}
                options={poolOptions}
              />
              <Field.HelperText>Currently {server.providerResourcePool || 'unset'}.</Field.HelperText>
            </Field.Root>
          </>
        )}
      </Stack>
    </Modal>
  )
}
