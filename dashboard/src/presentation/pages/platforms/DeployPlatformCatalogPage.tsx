import { Badge, Heading, HStack, Text } from '@chakra-ui/react'
import { ArrowRight, Boxes, Cpu, type LucideIcon } from 'lucide-react'
import { Link as RouterLink } from 'react-router-dom'
import { PageHeader } from '@/presentation/components/PageHeader'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import './platformDeployment.css'

interface PlatformCatalogEntry {
  kind: 'kubernetes' | 'slurm'
  label: string
  description: string
  icon: LucideIcon
  capabilities: readonly string[]
}
const PLATFORM_CATALOG: readonly PlatformCatalogEntry[] = [
  {
    kind: 'kubernetes',
    label: 'Kubernetes',
    description: 'Deploy a managed k0s runtime from a standalone node to a highly available control plane.',
    icon: Boxes,
    capabilities: ['k0s', 'Standalone or HA', 'AMD GPU Operator'],
  },
  {
    kind: 'slurm',
    label: 'Slurm',
    description: 'Build a GPU-aware HPC scheduler with explicit controller, compute, and login-node placement.',
    icon: Cpu,
    capabilities: ['HPC scheduling', 'GPU GRES', 'Optional Omnistat'],
  },
]

/**
 * Platform deployment entry route.
 *
 * Platform kinds are stable Dashboard presentation metadata rather than an API catalog. Each card
 * is a native link to its own deployment flow, which keeps Site scope and remains usable through
 * keyboard navigation, refresh, and copied URLs.
 */
export function DeployPlatformCatalogPage() {
  const { scopedHref } = useSiteScope()

  return (
    <div className="operator-page sw-platform-deploy-catalog">
      <PageHeader
        title="Deploy platform"
        subtitle="Choose the runtime Swallow should build. Kubernetes and Slurm use deployment flows tailored to their own topology and operating model."
        breadcrumbs={[{ label: 'Platforms', href: scopedHref('/platforms') }, { label: 'Deploy' }]}
      />

      <section aria-labelledby="platform-runtime-heading">
        <div className="sw-platform-deploy-catalog__heading">
          <Text className="sw-platform-deploy-eyebrow">Platform runtimes</Text>
          <Heading as="h2" id="platform-runtime-heading" size="xl">What do you want to deploy?</Heading>
          <Text color="fg.muted">Select a runtime to configure its Servers, features, and deployment-specific settings.</Text>
        </div>

        <div className="sw-platform-deploy-catalog__grid">
          {PLATFORM_CATALOG.map((entry) => {
            const Icon = entry.icon
            return (
              <RouterLink
                key={entry.kind}
                to={scopedHref(`/platforms/deploy/${entry.kind}`)}
                className="sw-platform-deploy-catalog-card"
                aria-label={`Configure ${entry.label}`}
              >
                <div className="sw-platform-deploy-catalog-card__topline">
                  <span className="sw-platform-deploy-catalog-card__icon" aria-hidden="true"><Icon size={27} /></span>
                  <Badge variant="subtle" colorPalette="brand">Managed runtime</Badge>
                </div>
                <div>
                  <Heading as="h3" size="xl">{entry.label}</Heading>
                  <Text color="fg.muted" mt="2">{entry.description}</Text>
                </div>
                <HStack gap="2" wrap="wrap" aria-label={`${entry.label} capabilities`}>
                  {entry.capabilities.map((capability) => (
                    <Badge key={capability} className="sw-platform-deploy-capability" variant="outline" colorPalette="gray">
                      {capability}
                    </Badge>
                  ))}
                </HStack>
                <div className="sw-platform-deploy-catalog-card__footer">
                  <Text fontWeight="semibold">Configure {entry.label}</Text>
                  <ArrowRight size={18} aria-hidden="true" />
                </div>
              </RouterLink>
            )
          })}
        </div>
      </section>
    </div>
  )
}
