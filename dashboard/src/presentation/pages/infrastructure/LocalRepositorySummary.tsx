import type { ReactNode } from 'react'
import { HStack, Stack, Text } from '@chakra-ui/react'
import { Link } from 'react-router-dom'
import { findCondition } from '@/domain/convergence/types'
import { isConverging, LocalRepositoryConditionTypes, type LocalRepository } from '@/domain/localRepository/types'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { CopyButton } from '@/presentation/components/CopyButton'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDateTime, formatRelative } from '@/shared/utils/time'
import { attentionTab, notReadyText, realizedPresentation } from './localRepositoryPresentation'
import { useIntegration } from './useIntegrationSettings'
import { LOCAL_REPOSITORY_POLL_MS } from './useLocalRepository'

/** A time shown relative, with the exact time as its title and the ISO value machine-readable. */
function ObservedTime({ at }: { at: string }) {
  return <time dateTime={at} title={formatDateTime(at)}>{formatRelative(at)}</time>
}

/** A labelled badge with its explanation below, the value shape of the status rows. */
function StatusValue({ status, label, children }: { status: string; label: string; children?: ReactNode }) {
  return (
    <Stack gap="1" align="flex-start">
      <StatusBadge status={status} label={label} />
      {children}
    </Stack>
  )
}

/**
 * The status block at the top of the Local repository page, all of it read from the API. What it
 * says depends on whether the spec serves deployments (`spec.serveDeployments`).
 *
 * While it does:
 * - Deployments: the API's `ready` and, when false, the `ready` condition's message, which lists
 *   everything in the way; the dashboard never works out readiness itself (see `notReadyText`).
 * - Provider: the `realized` condition (whether the provider serves every source, file, and image) with its
 *   message, and the spec generation it was judged for when the latest spec is not judged yet.
 * - A `requires_attention` condition puts an alert first that links to the Integration tab where the
 *   reason is fixed.
 *
 * While it does not, the worker converges nothing and records only `ready` `false` (`not_serving`),
 * so nothing here reads as a failure: Deployments says they fetch from the upstream sources, and
 * Provider says the repository is not synchronized and keeps what it holds. `realized` and
 * `requires_attention` are judgements from the last pass made while it served deployments, so they
 * are not shown; the worker judges again once deployments use it.
 *
 * Either way:
 * - User sources: the user's spec generation against the one the worker last converged, so a saved
 *   change that has not been observed reads as waiting, not as failed.
 * - The content origin Servers read the repository at (copyable), the Site's `repository`
 *   Integration (linked to its settings, with its Enabled or Paused switch), and when the worker
 *   last completed a pass. An old observation is a time, never a failure.
 *
 * `refreshError` is a failed background re-read of the repository, shown as a warning while the last
 * good read stays. The Integration is read once per id for its name; until then, or if that read
 * fails, the link shows the id.
 */
export function LocalRepositorySummary({ repository, refreshError }: { repository: LocalRepository; refreshError?: string }) {
  const { scopedHref } = useSiteScope()
  const { state: integrationState } = useIntegration(repository.integrationId)
  const integration = integrationState.status === 'ready' && integrationState.integration.id === repository.integrationId
    ? integrationState.integration
    : null
  const settingsHref = (tab: 'general' | 'connection' | 'credential') =>
    scopedHref(`/infrastructure/integrations/${encodeURIComponent(repository.integrationId)}/${tab}`, repository.siteId)

  const serving = repository.spec.serveDeployments
  const readyCondition = findCondition(repository.conditions, LocalRepositoryConditionTypes.ready)
  const realized = findCondition(repository.conditions, LocalRepositoryConditionTypes.realized)
  const attention = findCondition(repository.conditions, LocalRepositoryConditionTypes.requiresAttention)
  const realizedState = realizedPresentation(realized)
  const specGeneration = repository.spec.generation
  const specConverged = repository.observedGeneration >= specGeneration

  return (
    <SectionSurface
      title="Local repository"
      description="This Site's own copy of the packages, images, and files its deployments install."
      actions={
        isConverging(repository) ? (
          <Text color="fg.muted" fontSize="sm" role="status">
            Updating every {LOCAL_REPOSITORY_POLL_MS / 1000} s while it converges
          </Text>
        ) : undefined
      }
      className="sw-local-repository-section"
    >
      <Stack gap="4">
        {serving && attention?.status === 'true' && (
          <Alert status="error" title="The repository Integration needs attention">
            {attention.message || 'The worker cannot converge the Local Repository until the Integration is fixed.'}{' '}
            <Link to={settingsHref(attentionTab(attention.reason))}>Open the Integration settings</Link>
          </Alert>
        )}
        {refreshError && (
          <Alert status="warning" title="Updates interrupted">
            {refreshError} Showing the last successful read and retrying.
          </Alert>
        )}
        <DescriptionList
          items={[
            {
              label: 'Deployments',
              value: !serving ? (
                <StatusValue status="info" label="Upstream sources">
                  <Text fontSize="sm" color="fg.muted">Deployments fetch each artifact from the upstream source its module declares.</Text>
                </StatusValue>
              ) : (
                <StatusValue status={repository.ready ? 'ready' : 'warning'} label={repository.ready ? 'Ready' : 'Not ready'}>
                  <Text fontSize="sm" color="fg.muted">
                    {repository.ready
                      ? 'Deployments that fetch from the Local Repository are accepted.'
                      : notReadyText(readyCondition, repository.conditions.length > 0)}
                  </Text>
                </StatusValue>
              ),
            },
            {
              label: 'Provider',
              value: !serving ? (
                <StatusValue status="offline" label="Not synchronizing">
                  <Text fontSize="sm" color="fg.muted">
                    The repository is not synchronized while deployments use the upstream sources; what it already holds is kept.
                  </Text>
                </StatusValue>
              ) : (
                <StatusValue status={realizedState.badgeStatus} label={realizedState.label}>
                  {realized?.message && <Text fontSize="sm" color="fg.muted">{realized.message}</Text>}
                  {realized && realized.observedGeneration < specGeneration && (
                    <Text fontSize="xs" color="fg.muted">Judged for spec generation {realized.observedGeneration}; generation {specGeneration} is not checked yet.</Text>
                  )}
                </StatusValue>
              ),
            },
            {
              label: 'User sources',
              value: specConverged
                ? `Generation ${specGeneration}, converged by the worker`
                : serving
                  ? `Generation ${specGeneration} saved, not converged by the worker yet`
                  : `Generation ${specGeneration} saved, synchronized once deployments use this repository`,
            },
            {
              label: 'Content origin',
              value: repository.contentOrigin ? (
                <HStack gap="1">
                  <span className="sw-mono">{repository.contentOrigin}</span>
                  <CopyButton value={repository.contentOrigin} label="Copy content origin" />
                </HStack>
              ) : (
                <Text>
                  Not set. <Link to={settingsHref('connection')}>Set it on the Integration</Link>.
                </Text>
              ),
            },
            {
              label: 'Integration',
              value: (
                <HStack gap="2" wrap="wrap">
                  <Link to={settingsHref('general')}>{integration?.name ?? repository.integrationId}</Link>
                  {integration && (
                    <StatusBadge status={integration.enabled ? 'active' : 'offline'} label={integration.enabled ? 'Enabled' : 'Paused'} />
                  )}
                </HStack>
              ),
            },
            {
              label: 'Last observed',
              value: repository.observedAt ? <ObservedTime at={repository.observedAt} /> : 'Not observed yet',
            },
          ]}
        />
      </Stack>
    </SectionSurface>
  )
}
