import { platformLifecycleLabel } from '@/domain/platform/lifecycle'
import { platformCurrentSpec } from '@/domain/platform/spec'
import type { KubernetesTopology, Platform } from '@/domain/platform/types'
import type { PlatformModuleUI } from '@/modules/registry'
import type { MetricItem } from '@/presentation/components/ConsolePrimitives'
import { formatRelative } from '@/shared/utils/time'

/**
 * User label for a deployment topology value. Slurm reuses the same standalone/HA
 * vocabulary (its intent projects to these values), so this stays shared between both views.
 */
export function topologyLabel(topology: KubernetesTopology): string {
  switch (topology) {
    case 'standalone':
      return 'Standalone'
    case 'multi-node':
      return 'Multi-node (non-HA)'
    case 'high-availability':
      return 'High availability'
  }
}

/**
 * A Platform's topology label: the latest deployment Workflow's recorded intent; before one exists
 * (a `planned` Platform whose install has not run), the topology its current spec's member roles
 * describe, through the owning module's `specTopology`; `fallback` only when neither tells, for
 * example a registered Platform or one deployed before specs existed.
 */
export function platformTopologyLabel(platform: Platform, module: PlatformModuleUI | undefined, fallback: string): string {
  if (platform.deployment) return topologyLabel(platform.deployment.topology)
  const spec = platformCurrentSpec(platform)
  const derived = spec ? module?.specTopology?.(spec) : undefined
  return derived ? topologyLabel(derived) : fallback
}

/**
 * The lifecycle KPI shown first in every platform summary strip, regardless of type. Kept
 * here so both the Kubernetes and Slurm views open their MetricGrid the same way.
 */
export function platformLifecycleKpi(platform: Platform): MetricItem {
  return { label: 'Lifecycle', value: platformLifecycleLabel(platform.lifecycleState) }
}

/**
 * The membership-sync KPIs shown last in every platform summary strip. These describe the
 * membership axis (matched vs reported members, freshness) and are shared by both views; the
 * type-specific counts in between are owned by each view.
 */
export function platformSyncKpis(platform: Platform): MetricItem[] {
  return [
    {
      label: 'Matched members',
      value: platform.sync.matchedCount,
      detail: `${platform.sync.memberCount} reported`,
      tone: platform.sync.matchedCount < platform.sync.memberCount ? 'warning' : 'neutral',
    },
    {
      label: 'Last synced',
      value: platform.sync.lastSucceededAt ? formatRelative(platform.sync.lastSucceededAt) : 'No data',
    },
  ]
}
