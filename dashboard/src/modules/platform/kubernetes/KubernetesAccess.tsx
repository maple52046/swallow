import { Button, HStack, Text } from '@chakra-ui/react'
import { Link } from 'react-router-dom'
import { platformCurrentSpec, settingString } from '@/domain/platform/spec'
import type { PlatformAccessProps } from '@/modules/types'
import { CopyButton } from '@/presentation/components/CopyButton'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'

/** Compact Kubernetes access entry for the summary-first workspace Overview. */
export function KubernetesAccess({ platform }: PlatformAccessProps) {
  const { scopedHref } = useSiteScope()
  const spec = platformCurrentSpec(platform)
  const endpoint = settingString(spec?.settings, 'apiAddress') ?? settingString(spec?.settings, 'apiEndpoint')
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
      <HStack gap="2" wrap="wrap">
        <Text className="mono">{endpoint ?? 'API endpoint is not recorded'}</Text>
        {endpoint && <CopyButton value={endpoint} label="Copy Kubernetes API endpoint" />}
      </HStack>
    </SectionSurface>
  )
}
