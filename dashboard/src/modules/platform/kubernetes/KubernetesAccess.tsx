import { Button, HStack, Stack, Text } from '@chakra-ui/react'
import { Link } from 'react-router-dom'
import { platformCurrentSpec, settingString } from '@/domain/platform/spec'
import type { PlatformAccessProps } from '@/modules/types'
import { CopyButton } from '@/presentation/components/CopyButton'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useIntegration } from '@/presentation/pages/infrastructure/useIntegrationSettings'
import { SingleClusterCredentialNotice } from './SingleClusterCredentialNotice'

/**
 * Compact Kubernetes access entry for the summary-first workspace Overview: the API endpoint, the
 * way into live cluster management, and, for a Platform that still has one cluster-admin token, the
 * notice that says so.
 *
 * The endpoint is the one the deployment recorded on the Platform's credential Integration
 * (platforms.md: a non-HA control plane's address is known only once it is deployed). It reads
 * that Integration (`GET /api/v1/integrations/{id}`, admin-only like the Platform page) once per
 * id. Before an answer for the current id, and for a Platform without one, it falls back to the
 * HA virtual IP the spec records, so a standalone endpoint is never guessed.
 */
export function KubernetesAccess({ platform }: PlatformAccessProps) {
  const { scopedHref } = useSiteScope()
  const integrationId = platform.origin === 'deployed' ? platform.integrationId ?? undefined : undefined
  const { state } = useIntegration(integrationId)
  // The hook keeps its last answer while a new id loads, so only an answer for this id counts.
  const integration = integrationId && state.status === 'ready' && state.integration.id === integrationId
    ? state.integration
    : undefined
  const apiVip = settingString(platformCurrentSpec(platform)?.settings, 'apiVip')
  const endpoint = integration?.endpoint || (apiVip ? `https://${apiVip}:6443` : undefined)
  const managementAvailable = platform.origin === 'deployed' && Boolean(platform.integrationId)
  return (
    <SectionSurface
      title="Cluster access"
      description={managementAvailable
        ? 'Use the Kubernetes API endpoint for clients, or open live cluster management.'
        : 'The recorded API endpoint remains visible; live management requires a deployed Platform Integration.'}
      actions={managementAvailable ? (
        <Button asChild variant="outline" size="sm">
          <Link to={scopedHref(`/platforms/${encodeURIComponent(platform.id)}/cluster`, platform.siteId)}>Open cluster</Link>
        </Button>
      ) : undefined}
    >
      <Stack gap="3">
        <HStack gap="2" wrap="wrap">
          <Text className="mono">{endpoint ?? 'API endpoint is not recorded'}</Text>
          {endpoint && <CopyButton value={endpoint} label="Copy Kubernetes API endpoint" />}
        </HStack>
        {integration && <SingleClusterCredentialNotice platform={platform} integration={integration} />}
      </Stack>
    </SectionSurface>
  )
}
