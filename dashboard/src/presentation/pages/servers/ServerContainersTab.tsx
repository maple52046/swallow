import type { ReactNode } from 'react'
import { Button, HStack, Stack, Text } from '@chakra-ui/react'
import { ShieldCheck } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { hasSwallowInstalledDocker } from '@/domain/software/docker'
import { dockerApiEnabled, type SoftwareAssignment } from '@/domain/software/types'
import type { Server } from '@/domain/server/types'
import { DockerApiRiskNotice } from '@/presentation/components/DockerApiRiskNotice'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SectionSurface } from '@/presentation/components/OperatorPrimitives'
import { Alert } from '@/presentation/components/ui/alert'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { DockerExplorer } from './docker/DockerExplorer'
import { useDockerApiToggle } from './docker/useDockerApiToggle'
import { useServerDetailContext } from './useServerDetail'

/**
 * The Server detail "Containers" tab: Docker management for a Server where swallow installed Docker
 * CE (decision 043).
 *
 * What it shows is decided only by the swallow-owned Docker CE assignment shared by the detail page:
 *
 * - no swallow-installed Docker CE: an empty state pointing to Software (the tab is not listed, but a
 *   typed URL still lands here);
 * - a Workflow changing Docker CE (`pending`, `uninstalling`): progress with a link to the Workflow;
 *   the assignment is polled until it settles;
 * - the last apply `failed`: the failure with the Workflow link and a retry of the recorded spec;
 * - installed without `enableApi`: why management needs the API, the shared risk notice, and an
 *   action that enables it by re-running the install;
 * - installed with `enableApi`: the live Docker Host Explorer.
 *
 * Swallow never probes the host for an API enabled outside swallow, so neither does this tab.
 */
export function ServerContainersTab() {
  const { server, docker } = useServerDetailContext()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()

  if (docker.status === 'loading') return <LoadingState rows={4} />
  if (docker.status === 'error') return <ErrorState message={docker.message} onRetry={docker.reload} />
  const assignment = docker.assignment
  if (!hasSwallowInstalledDocker(assignment)) {
    return (
      <EmptyState
        title="Docker CE is not installed by swallow"
        message="Install Docker CE on this Server from Software to manage its images, containers, volumes, and networks here."
        action={{ label: 'Open Software', onClick: () => navigate(scopedHref('/software')) }}
      />
    )
  }

  const openWorkflow = () => navigate(scopedHref(`/workflows/${assignment.lastWorkflowId}`))
  const workflowButton = assignment.lastWorkflowId ? (
    <Button size="sm" variant="outline" onClick={openWorkflow}>
      View workflow
    </Button>
  ) : null

  if (assignment.state === 'pending' || assignment.state === 'uninstalling') {
    return (
      <Alert
        status="info"
        title={assignment.state === 'pending' ? 'Applying Docker CE' : 'Uninstalling Docker CE'}
        actions={workflowButton}
      >
        {assignment.state === 'pending'
          ? 'swallow is applying the Docker CE configuration on this Server. This tab updates when the Workflow finishes.'
          : 'swallow is removing Docker CE from this Server.'}
      </Alert>
    )
  }
  if (assignment.state === 'failed') {
    return <DockerApplyFailed server={server} assignment={assignment} workflowButton={workflowButton} onStarted={docker.reload} />
  }
  if (!dockerApiEnabled(assignment.spec)) {
    return <DockerApiDisabledPanel server={server} assignment={assignment} onStarted={docker.reload} />
  }
  return <DockerExplorer server={server} assignment={assignment} onAssignmentChanged={docker.reload} />
}

/** Lock reason for the Docker CE re-apply actions; the install API refuses locked Servers. */
function reapplyBlockedReason(server: Server): string | undefined {
  return server.provisioning?.locked ? 'Unlock the Server before changing Docker CE on it.' : undefined
}

/**
 * Docker CE is installed but its Engine API is off (or the record predates the option). Explains
 * that management needs the API, states the risk with the shared notice, and offers to enable it by
 * re-applying Docker CE with `enableApi: true`.
 */
function DockerApiDisabledPanel({ server, assignment, onStarted }: { server: Server; assignment: SoftwareAssignment; onStarted: () => void }) {
  const toggle = useDockerApiToggle(server.id, assignment, onStarted)
  const blocked = reapplyBlockedReason(server)
  return (
    <SectionSurface
      title="Docker management needs the Docker Engine API"
      description="swallow installed Docker CE on this Server without its Engine API, so it cannot list or change images, containers, volumes, or networks here."
    >
      <Stack gap="4">
        <DockerApiRiskNotice context="enable" />
        {toggle.error && (
          <Alert status="error" title="The Docker Engine API could not be enabled">
            {toggle.error}
          </Alert>
        )}
        {blocked && (
          <Text color="fg.muted" fontSize="sm">
            {blocked}
          </Text>
        )}
        <HStack>
          <Button
            colorPalette="brand"
            onClick={() => void toggle.setApiEnabled(true)}
            loading={toggle.submitting}
            disabled={blocked !== undefined || toggle.submitting}
          >
            <ShieldCheck size={16} aria-hidden /> Enable Docker API
          </Button>
        </HStack>
      </Stack>
    </SectionSurface>
  )
}

/**
 * The last Docker CE apply failed. Docker may still be installed, but swallow cannot confirm its
 * configuration, so the explorer is not offered; the operator can inspect the Workflow or retry the
 * recorded spec.
 */
function DockerApplyFailed({
  server,
  assignment,
  workflowButton,
  onStarted,
}: {
  server: Server
  assignment: SoftwareAssignment
  workflowButton: ReactNode
  onStarted: () => void
}) {
  const toggle = useDockerApiToggle(server.id, assignment, onStarted)
  const blocked = reapplyBlockedReason(server)
  return (
    <Stack gap="4">
      <Alert
        status="error"
        title="The last Docker CE apply failed"
        actions={
          <HStack gap="2">
            {workflowButton}
            <Button
              size="sm"
              colorPalette="red"
              variant="outline"
              onClick={() => void toggle.setApiEnabled(dockerApiEnabled(assignment.spec))}
              loading={toggle.submitting}
              disabled={blocked !== undefined || toggle.submitting}
            >
              Retry
            </Button>
          </HStack>
        }
      >
        Docker CE may still be installed, but swallow could not confirm its configuration. Check the Workflow, then retry.
        {blocked ? ` ${blocked}` : ''}
      </Alert>
      {toggle.error && (
        <Alert status="error" title="Docker CE could not be re-applied">
          {toggle.error}
        </Alert>
      )}
      {dockerApiEnabled(assignment.spec) && <DockerApiRiskNotice context="enable" />}
    </Stack>
  )
}
