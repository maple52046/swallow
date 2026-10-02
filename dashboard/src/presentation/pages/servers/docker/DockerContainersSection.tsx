import { useCallback, useState } from 'react'
import { Button, HStack, Stack, Text } from '@chakra-ui/react'
import { Play, Plus, RefreshCw, RotateCw, ScrollText, Square, Trash2 } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import { formatDockerPort, shortDockerId, type DockerContainer, type DockerContainerAction } from '@/domain/software/docker'
import { formatDateTime, formatRelative } from '@/shared/utils/time'
import { AsyncSection } from '@/presentation/components/AsyncSection'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { CopyButton } from '@/presentation/components/CopyButton'
import { EmptyState } from '@/presentation/components/EmptyState'
import { LogViewer } from '@/presentation/components/LogViewer'
import { SectionSurface } from '@/presentation/components/OperatorPrimitives'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { ResponsiveResourceList } from '@/presentation/components/ResponsiveResourceList'
import { CreateContainerDialog } from './CreateContainerDialog'
import {
  DOCKER_EXPLORER_UNAVAILABLE_HINT,
  canStartContainer,
  canStopContainer,
  containerStateBadgeStatus,
  type DockerSectionProps,
} from './dockerPresentation'
import { useDockerMutation } from './useDockerMutation'

// Lifecycle verbs and their feedback, so each row action reads the same everywhere.
const ACTION_COPY: Record<DockerContainerAction, { label: string; success: string; failure: string }> = {
  start: { label: 'Start', success: 'Container started', failure: 'Container could not be started' },
  stop: { label: 'Stop', success: 'Container stopped', failure: 'Container could not be stopped' },
  restart: { label: 'Restart', success: 'Container restarted', failure: 'Container could not be restarted' },
}

// A bounded snapshot is enough for diagnosis and keeps the dialog responsive; the API caps at 2000.
const LOG_TAIL_LINES = 500

/**
 * Containers section of the Docker Host Explorer (P1): every container on the host, running or not,
 * with create, start/stop/restart, a log snapshot, and remove.
 *
 * State comes straight from the Engine and is shown with its own text (`running`, `exited`, ...);
 * colour only reinforces it. Lifecycle buttons are offered only where they make sense for the
 * current state, one write runs at a time, and every write re-reads the list. Remove confirms first
 * and explains the `force` and anonymous-volume options. Writes are disabled while the Server is
 * locked.
 */
export function DockerContainersSection({ serverId, readOnlyReason, onChanged }: DockerSectionProps) {
  const { dockerHosts } = useApp()
  const containers = useAsyncData(() => dockerHosts.listContainers(serverId), [serverId])
  const reloadContainers = containers.reload
  const changed = useCallback(() => {
    reloadContainers()
    onChanged()
  }, [reloadContainers, onChanged])
  const { busyKey, run } = useDockerMutation(changed)
  const [creating, setCreating] = useState(false)
  const [logsFor, setLogsFor] = useState<DockerContainer | null>(null)
  const [pendingRemove, setPendingRemove] = useState<DockerContainer | null>(null)
  const [forceRemove, setForceRemove] = useState(false)
  const [removeVolumes, setRemoveVolumes] = useState(false)
  const readOnly = readOnlyReason !== undefined

  const act = (container: DockerContainer, action: DockerContainerAction) =>
    void run(`${action}:${container.id}`, () => dockerHosts.actOnContainer(serverId, container.id, action), {
      success: ACTION_COPY[action].success,
      failure: ACTION_COPY[action].failure,
      target: container.name,
    })

  const remove = async () => {
    if (!pendingRemove) return
    await run(
      `remove:${pendingRemove.id}`,
      () => dockerHosts.removeContainer(serverId, pendingRemove.id, { force: forceRemove, removeVolumes }),
      { success: 'Container removed', failure: 'Container could not be removed', target: pendingRemove.name },
    )
    setPendingRemove(null)
  }

  const actionButton = (container: DockerContainer, action: DockerContainerAction, Icon: typeof Play) => (
    <Button
      key={action}
      size="sm"
      variant="outline"
      disabled={readOnly || busyKey !== null}
      loading={busyKey === `${action}:${container.id}`}
      aria-label={`${ACTION_COPY[action].label} container ${container.name}`}
      onClick={() => act(container, action)}
    >
      <Icon size={16} aria-hidden /> {ACTION_COPY[action].label}
    </Button>
  )

  return (
    <SectionSurface
        title="Containers"
        description="Every container on this host, running or not, read live from its Docker Engine."
        actions={
          <HStack gap="2">
            <Button size="sm" colorPalette="brand" onClick={() => setCreating(true)} disabled={readOnly}>
              <Plus size={16} aria-hidden /> Create container
            </Button>
            <Button variant="plain" size="sm" onClick={containers.reload}>
              <RefreshCw size={16} aria-hidden /> Refresh
            </Button>
          </HStack>
        }
      >
      <AsyncSection state={containers} unavailableTitle="Docker explorer is unavailable" unavailableHint={DOCKER_EXPLORER_UNAVAILABLE_HINT}>
        {(items) =>
          items.length === 0 ? (
            <EmptyState title="No containers" message="This host has no containers. Create one from a local image." />
          ) : (
            <ResponsiveResourceList
              label="Docker containers"
              items={items}
              rowKey={(container) => container.id}
              identityHeader="Container"
              title={(container) => container.name || shortDockerId(container.id)}
              subtitle={(container) => (
                <span className="sw-cell-inline">
                  {shortDockerId(container.id)}
                  <CopyButton value={container.id} label={`Copy container id of ${container.name}`} />
                </span>
              )}
              status={{
                header: 'State',
                cell: (container) => (
                  <Stack gap="0.5" align="flex-start">
                    <StatusBadge status={containerStateBadgeStatus(container.state)} label={container.state || 'unknown'} />
                    {container.status && (
                      <Text color="fg.muted" fontSize="xs">
                        {container.status}
                      </Text>
                    )}
                  </Stack>
                ),
              }}
              columns={[
                { header: 'Image', cell: (container) => <span className="mono">{container.image}</span> },
                {
                  header: 'Ports',
                  cell: (container) =>
                    container.ports.length === 0 ? (
                      '—'
                    ) : (
                      <Stack gap="0">
                        {container.ports.map((port) => (
                          <span key={formatDockerPort(port)} className="mono">
                            {formatDockerPort(port)}
                          </span>
                        ))}
                      </Stack>
                    ),
                },
                {
                  header: 'Created',
                  cell: (container) => <span title={formatDateTime(container.createdAt)}>{formatRelative(container.createdAt)}</span>,
                },
              ]}
              actions={(container) => (
                <>
                  {canStartContainer(container.state) && actionButton(container, 'start', Play)}
                  {canStopContainer(container.state) && actionButton(container, 'stop', Square)}
                  {canStopContainer(container.state) && actionButton(container, 'restart', RotateCw)}
                  <Button size="sm" variant="outline" aria-label={`Logs of container ${container.name}`} onClick={() => setLogsFor(container)}>
                    <ScrollText size={16} aria-hidden /> Logs
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    colorPalette="red"
                    disabled={readOnly || busyKey !== null}
                    loading={busyKey === `remove:${container.id}`}
                    aria-label={`Remove container ${container.name}`}
                    onClick={() => {
                      setForceRemove(false)
                      setRemoveVolumes(false)
                      setPendingRemove(container)
                    }}
                  >
                    <Trash2 size={16} aria-hidden /> Remove
                  </Button>
                </>
              )}
            />
          )
        }
      </AsyncSection>

      {creating && (
        <CreateContainerDialog
          serverId={serverId}
          onClose={() => setCreating(false)}
          onCreated={() => {
            setCreating(false)
            changed()
          }}
        />
      )}
      {logsFor && <ContainerLogsDialog serverId={serverId} container={logsFor} onClose={() => setLogsFor(null)} />}
      <ConfirmDialog
        open={pendingRemove !== null}
        title="Remove container"
        confirmLabel="Remove container"
        busy={pendingRemove !== null && busyKey === `remove:${pendingRemove.id}`}
        onCancel={() => setPendingRemove(null)}
        onConfirm={() => void remove()}
      >
        <Stack gap="3">
          <Text>
            Removing <strong>{pendingRemove?.name}</strong> deletes the container and its writable layer. This cannot be
            undone. Named volumes are kept.
          </Text>
          <Checkbox checked={forceRemove} onCheckedChange={setForceRemove}>
            Force: kill the container first if it is running
          </Checkbox>
          <Checkbox checked={removeVolumes} onCheckedChange={setRemoveVolumes}>
            Also remove its anonymous volumes
          </Checkbox>
        </Stack>
      </ConfirmDialog>
    </SectionSurface>
  )
}

/**
 * A bounded snapshot of one container's stdout and stderr (not a live stream). Refresh re-reads the
 * latest lines; the shared LogViewer provides search, copy, and download.
 */
function ContainerLogsDialog({ serverId, container, onClose }: { serverId: string; container: DockerContainer; onClose: () => void }) {
  const { dockerHosts } = useApp()
  const logs = useAsyncData(() => dockerHosts.containerLogs(serverId, container.id, LOG_TAIL_LINES), [serverId, container.id])
  return (
    <Modal
      open
      onClose={onClose}
      size="xl"
      title={`Logs — ${container.name}`}
      description={`The last ${LOG_TAIL_LINES} lines of standard output and standard error (a snapshot, not a live stream).`}
      footer={
        <Button variant="ghost" onClick={onClose}>
          Close
        </Button>
      }
    >
      <AsyncSection state={logs} unavailableTitle="Docker explorer is unavailable" unavailableHint={DOCKER_EXPLORER_UNAVAILABLE_HINT}>
        {(text) => (
          <LogViewer
            text={text || 'No log output.'}
            label="container logs"
            downloadName={`swallow-${container.name || shortDockerId(container.id)}-logs`}
            onRefresh={logs.reload}
            height={420}
          />
        )}
      </AsyncSection>
    </Modal>
  )
}
