import { useCallback, useState } from 'react'
import { Button, Field, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import { DOCKER_OBJECT_NAME_PATTERN, type DockerVolume } from '@/domain/software/docker'
import { formatDateTime, formatRelative } from '@/shared/utils/time'
import { AsyncSection } from '@/presentation/components/AsyncSection'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { CopyButton } from '@/presentation/components/CopyButton'
import { EmptyState } from '@/presentation/components/EmptyState'
import { SectionSurface } from '@/presentation/components/OperatorPrimitives'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { ResponsiveResourceList } from '@/presentation/components/ResponsiveResourceList'
import { DOCKER_EXPLORER_UNAVAILABLE_HINT, type DockerSectionProps } from './dockerPresentation'
import { useDockerMutation } from './useDockerMutation'

/**
 * Volumes section of the Docker Host Explorer (P2): the host's volumes live from the Engine, with
 * create and remove. Removal is permanent data loss, so the confirmation says so; a volume still used
 * by a container is refused by the Engine and its reason is shown. Writes are disabled while the
 * Server is locked.
 */
export function DockerVolumesSection({ serverId, readOnlyReason, onChanged }: DockerSectionProps) {
  const { dockerHosts } = useApp()
  const volumes = useAsyncData(() => dockerHosts.listVolumes(serverId), [serverId])
  const reloadVolumes = volumes.reload
  const changed = useCallback(() => {
    reloadVolumes()
    onChanged()
  }, [reloadVolumes, onChanged])
  const { busyKey, run } = useDockerMutation(changed)
  const [creating, setCreating] = useState(false)
  const [pendingRemove, setPendingRemove] = useState<DockerVolume | null>(null)
  const readOnly = readOnlyReason !== undefined

  const remove = async () => {
    if (!pendingRemove) return
    await run(`remove:${pendingRemove.name}`, () => dockerHosts.removeVolume(serverId, pendingRemove.name, false), {
      success: 'Volume removed',
      failure: 'Volume could not be removed',
      target: pendingRemove.name,
    })
    setPendingRemove(null)
  }

  return (
    <SectionSurface
        title="Volumes"
        description="Persistent volumes on this host, read live from its Docker Engine."
        actions={
          <HStack gap="2">
            <Button size="sm" colorPalette="brand" onClick={() => setCreating(true)} disabled={readOnly}>
              <Plus size={16} aria-hidden /> Create volume
            </Button>
            <Button variant="plain" size="sm" onClick={volumes.reload}>
              <RefreshCw size={16} aria-hidden /> Refresh
            </Button>
          </HStack>
        }
      >
      <AsyncSection state={volumes} unavailableTitle="Docker explorer is unavailable" unavailableHint={DOCKER_EXPLORER_UNAVAILABLE_HINT}>
        {(items) =>
          items.length === 0 ? (
            <EmptyState title="No volumes" message="This host has no volumes." />
          ) : (
            <ResponsiveResourceList
              label="Docker volumes"
              items={items}
              rowKey={(volume) => volume.name}
              identityHeader="Volume"
              title={(volume) => (
                <span className="sw-cell-inline">
                  {volume.name}
                  <CopyButton value={volume.name} label={`Copy volume name ${volume.name}`} />
                </span>
              )}
              columns={[
                { header: 'Driver', cell: (volume) => volume.driver || '—' },
                { header: 'Mountpoint', cell: (volume) => <span className="mono">{volume.mountpoint || '—'}</span> },
                {
                  header: 'Created',
                  cell: (volume) =>
                    volume.createdAt ? <span title={formatDateTime(volume.createdAt)}>{formatRelative(volume.createdAt)}</span> : '—',
                },
              ]}
              actions={(volume) => (
                <Button
                  size="sm"
                  variant="outline"
                  colorPalette="red"
                  disabled={readOnly || busyKey !== null}
                  loading={busyKey === `remove:${volume.name}`}
                  aria-label={`Remove volume ${volume.name}`}
                  onClick={() => setPendingRemove(volume)}
                >
                  <Trash2 size={16} aria-hidden /> Remove
                </Button>
              )}
            />
          )
        }
      </AsyncSection>

      {creating && (
        <CreateVolumeDialog
          serverId={serverId}
          onClose={() => setCreating(false)}
          onCreated={() => {
            setCreating(false)
            changed()
          }}
        />
      )}
      <ConfirmDialog
        open={pendingRemove !== null}
        title="Remove volume"
        confirmLabel="Remove volume"
        busy={pendingRemove !== null && busyKey === `remove:${pendingRemove.name}`}
        onCancel={() => setPendingRemove(null)}
        onConfirm={() => void remove()}
      >
        <Text>
          Removing <strong>{pendingRemove?.name}</strong> permanently deletes the data stored in it. This cannot be undone.
          A volume used by a container cannot be removed.
        </Text>
      </ConfirmDialog>
    </SectionSurface>
  )
}

/** Create-volume form: an optional name (the Engine generates one) and a driver (default local). */
function CreateVolumeDialog({ serverId, onClose, onCreated }: { serverId: string; onClose: () => void; onCreated: () => void }) {
  const { dockerHosts } = useApp()
  const { showToast } = useToast()
  const [name, setName] = useState('')
  const [driver, setDriver] = useState('local')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const nameInvalid = name.trim() !== '' && !DOCKER_OBJECT_NAME_PATTERN.test(name.trim())

  const submit = async () => {
    if (nameInvalid || submitting) return
    setSubmitting(true)
    setError('')
    try {
      const volume = await dockerHosts.createVolume(serverId, {
        ...(name.trim() ? { name: name.trim() } : {}),
        driver: driver.trim() || 'local',
      })
      showToast({ tone: 'success', title: 'Volume created', description: volume.name })
      onCreated()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The volume could not be created.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={() => !submitting && onClose()}
      closeOnInteractOutside={!submitting}
      title="Create volume"
      description="A volume keeps container data on this host independently of any container."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={nameInvalid || submitting}>
            Create
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="The volume could not be created">
            {error}
          </Alert>
        )}
        <Field.Root invalid={nameInvalid}>
          <Field.Label>Name</Field.Label>
          <Input value={name} onChange={(event) => setName(event.target.value)} placeholder="web-data" autoFocus />
          <Field.HelperText>Optional; the Engine generates one when empty.</Field.HelperText>
          <Field.ErrorText>Use letters, digits, and _ . - (starting with a letter or digit).</Field.ErrorText>
        </Field.Root>
        <Field.Root>
          <Field.Label>Driver</Field.Label>
          <Input value={driver} onChange={(event) => setDriver(event.target.value)} placeholder="local" />
        </Field.Root>
      </Stack>
    </Modal>
  )
}
