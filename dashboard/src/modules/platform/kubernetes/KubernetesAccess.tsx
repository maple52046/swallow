import { Button, HStack, Stack, Text } from '@chakra-ui/react'
import { Link } from 'react-router-dom'
import { platformCurrentSpec, settingString } from '@/domain/platform/spec'
import { isAdmin } from '@/domain/user/access'
import type { PlatformAccessProps } from '@/modules/types'
import { CopyButton } from '@/presentation/components/CopyButton'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useIntegration } from '@/presentation/pages/infrastructure/useIntegrationSettings'
import { SingleClusterCredentialNotice } from './SingleClusterCredentialNotice'

/**
 * Compact Kubernetes access entry for the summary-first workspace Overview: the API endpoint, the
 * way into live cluster management, and, for a Platform that still has one cluster-admin token, the
 * notice that says so.
 *
 * The endpoint is the one the deployment recorded on the Platform's credential Integration
 * (platforms.md: a non-HA control plane's address is known only once it is deployed). An admin's
 * page reads that Integration (`GET /api/v1/integrations/{id}`, an admin route) once per id. Before
 * an answer for the current id, for a Platform without one, and for every other user, it falls back
 * to the HA virtual IP the spec records, so a standalone endpoint is never guessed. Live cluster
 * management needs the Pool operate class, so only a user who may manage the Platform gets the way in.
 */
export function KubernetesAccess({ platform, canOperate }: PlatformAccessProps) {
  const { scopedHref } = useSiteScope()
  const { currentUser } = useAuth()
  const admin = isAdmin(currentUser)
  const integrationId = admin && platform.origin === 'deployed' ? platform.integrationId ?? undefined : undefined
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
      description={!canOperate
        ? 'Use the Kubernetes API endpoint for clients. Live cluster management is for the Pool’s owners and admins.'
        : managementAvailable
          ? 'Use the Kubernetes API endpoint for clients, or open live cluster management.'
          : 'The recorded API endpoint remains visible; live management requires a deployed Platform Integration.'}
      actions={managementAvailable && canOperate ? (
        <Button asChild variant="outline" size="sm">
          <Link to={scopedHref(`/platforms/${encodeURIComponent(platform.id)}/cluster`, platform.siteId)}>Open cluster</Link>
        </Button>
      ) : undefined}
    >
      <Stack gap="3">
        <HStack gap="2" wrap="wrap">
          <Text className="mono">{endpoint ?? (admin ? 'API endpoint is not recorded' : 'An admin can tell you the API endpoint')}</Text>
          {endpoint && <CopyButton value={endpoint} label="Copy Kubernetes API endpoint" />}
        </HStack>
        {integration && <SingleClusterCredentialNotice platform={platform} integration={integration} />}
      </Stack>
    </SectionSurface>
  )
}
