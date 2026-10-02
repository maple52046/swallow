import { useState } from 'react'
import { Button, HStack, Stack, Tabs, Text } from '@chakra-ui/react'
import { Box, Boxes, HardDrive, Layers, RefreshCw, ShieldOff, Waypoints } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import type { Server } from '@/domain/server/types'
import type { SoftwareAssignment } from '@/domain/software/types'
import { formatBytes } from '@/shared/utils/bytes'
import { AsyncSection } from '@/presentation/components/AsyncSection'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { CopyButton } from '@/presentation/components/CopyButton'
import { MetricGrid, SectionSurface } from '@/presentation/components/OperatorPrimitives'
import { Alert } from '@/presentation/components/ui/alert'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { DockerContainersSection } from './DockerContainersSection'
import { DockerImagesSection } from './DockerImagesSection'
import { DockerNetworksSection } from './DockerNetworksSection'
import { DockerVolumesSection } from './DockerVolumesSection'
import { DOCKER_EXPLORER_UNAVAILABLE_HINT, LOCKED_DOCKER_READ_ONLY_REASON } from './dockerPresentation'
import { useDockerApiToggle } from './useDockerApiToggle'

interface DockerExplorerProps {
  server: Server
  /** The installed Docker CE assignment with `enableApi: true`; the caller has checked both. */
  assignment: SoftwareAssignment
  /** Re-reads the assignment after a Workflow was started from here (disabling the API). */
  onAssignmentChanged: () => void
}

/**
 * The Docker Host Explorer for one Server (decision 043): the live Engine header plus Containers
 * (shown first), Images, Volumes, and Networks sections, all read through api-server and never
 * stored.
 *
 * Sections mount lazily, so only the one on screen talks to the Engine, and any section write
 * refreshes the header counts. A locked Server stays fully readable while every write control is
 * disabled with the reason shown once at the top. "Disable Docker API" re-applies Docker CE without
 * the listener after a confirmation that names the daemon restart; the tab then follows the
 * Workflow until the explorer is no longer offered.
 */
export function DockerExplorer({ server, assignment, onAssignmentChanged }: DockerExplorerProps) {
  const { dockerHosts } = useApp()
  const summary = useAsyncData(() => dockerHosts.summary(server.id), [server.id])
  const readOnlyReason = server.provisioning?.locked ? LOCKED_DOCKER_READ_ONLY_REASON : undefined
  const [section, setSection] = useState('containers')
  const [confirmDisable, setConfirmDisable] = useState(false)
  const toggle = useDockerApiToggle(server.id, assignment, onAssignmentChanged)
  const endpoint = summary.status === 'ready' ? summary.data.endpoint : ''

  return (
    <Stack gap="5">
      {readOnlyReason && (
        <Alert status="warning" title="Read-only while locked">
          {readOnlyReason}
        </Alert>
      )}
      {toggle.error && (
        <Alert status="error" title="The Docker Engine API could not be disabled">
          {toggle.error}
        </Alert>
      )}
      <SectionSurface
        title="Docker Engine"
        description="Live state of this host's Docker Engine, read through swallow. Nothing here is stored by swallow."
        actions={
          <HStack gap="2">
            <Button
              size="sm"
              variant="outline"
              onClick={() => setConfirmDisable(true)}
              disabled={readOnlyReason !== undefined || toggle.submitting}
            >
              <ShieldOff size={16} aria-hidden /> Disable Docker API
            </Button>
            <Button variant="plain" size="sm" onClick={summary.reload}>
              <RefreshCw size={16} aria-hidden /> Refresh
            </Button>
          </HStack>
        }
      >
        <AsyncSection state={summary} unavailableTitle="Docker explorer is unavailable" unavailableHint={DOCKER_EXPLORER_UNAVAILABLE_HINT}>
          {(data) => (
            <Stack gap="3">
              <MetricGrid
                items={[
                  { label: 'Engine', value: data.serverVersion || 'unknown', detail: data.apiVersion ? `API ${data.apiVersion}` : undefined, icon: <Box size={16} /> },
                  {
                    label: 'Containers',
                    value: `${data.containersRunning} / ${data.containers}`,
                    detail: `running · ${data.containersPaused} paused · ${data.containersStopped} stopped`,
                    icon: <Boxes size={16} />,
                  },
                  { label: 'Images', value: data.images, icon: <Layers size={16} /> },
                  {
                    label: 'Host',
                    value: data.operatingSystem || 'unknown',
                    detail: [data.architecture, data.cpus ? `${data.cpus} CPUs` : '', data.memoryBytes > 0 ? formatBytes(data.memoryBytes) : '']
                      .filter(Boolean)
                      .join(' · '),
                    icon: <HardDrive size={16} />,
                  },
                ]}
              />
              <Text color="fg.muted" fontSize="sm" className="sw-cell-inline">
                Engine API endpoint <span className="mono">{data.endpoint}</span>
                <CopyButton value={data.endpoint} label="Copy Docker Engine API endpoint" />
              </Text>
            </Stack>
          )}
        </AsyncSection>
      </SectionSurface>

      <Tabs.Root
        value={section}
        onValueChange={(details) => setSection(details.value)}
        lazyMount
        unmountOnExit
      >
        <Tabs.List aria-label="Docker objects">
          <Tabs.Trigger value="containers">
            <Boxes size={16} aria-hidden /> Containers
          </Tabs.Trigger>
          <Tabs.Trigger value="images">
            <Layers size={16} aria-hidden /> Images
          </Tabs.Trigger>
          <Tabs.Trigger value="volumes">
            <HardDrive size={16} aria-hidden /> Volumes
          </Tabs.Trigger>
          <Tabs.Trigger value="networks">
            <Waypoints size={16} aria-hidden /> Networks
          </Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content value="containers">
          <DockerContainersSection serverId={server.id} readOnlyReason={readOnlyReason} onChanged={summary.reload} />
        </Tabs.Content>
        <Tabs.Content value="images">
          <DockerImagesSection serverId={server.id} readOnlyReason={readOnlyReason} onChanged={summary.reload} />
        </Tabs.Content>
        <Tabs.Content value="volumes">
          <DockerVolumesSection serverId={server.id} readOnlyReason={readOnlyReason} onChanged={summary.reload} />
        </Tabs.Content>
        <Tabs.Content value="networks">
          <DockerNetworksSection serverId={server.id} readOnlyReason={readOnlyReason} onChanged={summary.reload} />
        </Tabs.Content>
      </Tabs.Root>

      <ConfirmDialog
        open={confirmDisable}
        title="Disable the Docker Engine API"
        confirmLabel="Disable Docker API"
        busy={toggle.submitting}
        onCancel={() => setConfirmDisable(false)}
        onConfirm={() => {
          void toggle.setApiEnabled(false).then(() => setConfirmDisable(false))
        }}
      >
        <Stack gap="2">
          <Text>
            swallow re-applies Docker CE on this Server without the API listener{endpoint ? ` (${endpoint})` : ''}. The
            Docker daemon restarts, so containers without a restart policy stop.
          </Text>
          <Text>
            Images, containers, volumes, and networks stay on the host, but this tab can no longer manage them until the API
            is enabled again.
          </Text>
        </Stack>
      </ConfirmDialog>
    </Stack>
  )
}
