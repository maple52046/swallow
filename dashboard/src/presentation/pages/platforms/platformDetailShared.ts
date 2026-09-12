import { platformLifecycleLabel } from '@/domain/platform/lifecycle'
import type { KubernetesTopology, Platform } from '@/domain/platform/types'
import type { MetricItem } from '@/presentation/components/OperatorPrimitives'
import { formatRelative } from '@/shared/utils/time'

/**
 * Operator label for a deployment topology value. Slurm reuses the same standalone/HA
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
