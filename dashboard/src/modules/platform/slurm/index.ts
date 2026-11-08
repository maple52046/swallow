import { Cpu, LayoutGrid, Server } from 'lucide-react'
import { wantsPlatformGrafana } from '@/domain/platform/monitoring'
import type { PlatformModuleUI } from '@/modules/types'
import { slurmPlatformFacts, slurmSpecTopology } from './slurmFacts'
import { hardwareCounterStates } from './slurmHardwareCounters'
import { SlurmAccess } from './SlurmAccess'
import { SlurmHardwareCountersSection } from './SlurmHardwareCountersSection'
import { SlurmNodesRoute, SlurmPartitionsRoute } from './SlurmWorkspaceRoutes'
import { useSlurmDeployForm } from './useSlurmDeployForm'
import { SlurmSettingsPage } from './SlurmSettingsPage'

/**
 * The Slurm module's UI: its deploy form, access summary, native workspace routes, configuration
 * facts, the Configuration section that switches Omnistat's hardware counters on a deployed
 * Platform, and monitoring contributions (Omnistat with its per-node hardware counters, or the
 * Device Metrics Exporter).
 */
export const slurmModule: PlatformModuleUI = {
  kind: 'platform',
  id: 'slurm',
  label: 'Slurm',
  icon: Cpu,
  capabilities: ['Batch scheduling', 'Login access', 'Partitions and compute nodes'],
  badgePalette: 'gray',
  Access: SlurmAccess,
  monitoringEntries: (platform) => {
    const grafana = wantsPlatformGrafana(platform.spec)
    const labels = {
      installing: 'Installing',
      ready: 'Ready',
      failed: 'Failed',
      absent: 'Removed',
    } as const
    const detail = (version: string, installMethod: 'native' | 'container') => [
      version,
      'Collected by the Site Prometheus',
      ...(grafana ? [`Platform Grafana installed ${installMethod === 'native' ? 'natively' : 'as a container'} on the primary controller`] : []),
    ].join(' · ')
    const counters = platform.omnistat ? hardwareCounterStates(platform.omnistat) : undefined
    return [
      ...(platform.omnistat ? [{
        name: 'Omnistat',
        state: platform.omnistat.state,
        statusLabel: labels[platform.omnistat.state],
        detail: detail(platform.omnistat.version, platform.omnistat.installMethod),
        workflowId: platform.omnistat.lastWorkflowId,
        serverId: platform.omnistat.observabilityServerId,
        ...(counters ? { serverStates: counters } : {}),
      }] : []),
      ...(platform.deviceMetricsExporter ? [{
        name: 'Device Metrics Exporter',
        state: platform.deviceMetricsExporter.state,
        statusLabel: labels[platform.deviceMetricsExporter.state],
        detail: detail(platform.deviceMetricsExporter.version, platform.deviceMetricsExporter.installMethod),
        workflowId: platform.deviceMetricsExporter.lastWorkflowId,
        serverId: platform.deviceMetricsExporter.observabilityServerId,
        ...(platform.deviceMetricsExporter.gpuIdentity === 'collapsed' && platform.deviceMetricsExporter.state !== 'absent'
          ? { warning: {
              title: 'GPU identity collapsed',
              message: "At least one node's exporter reports fewer distinct GPU ids than it has GPUs. GPU metrics are still collected, but job dashboards can attribute a job to the wrong GPUs; Slurm GPU allocation and isolation are not affected.",
            } }
          : {}),
      }] : []),
    ]
  },
  tabs: [
    { value: 'nodes', label: 'Nodes', icon: Server, available: () => true, requiresMembers: true, Content: SlurmNodesRoute },
    { value: 'partitions', label: 'Partitions', icon: LayoutGrid, available: () => true, requiresMembers: true, Content: SlurmPartitionsRoute },
  ],
  deploy: { icon: Cpu, namePlaceholder: 'lab-slurm', useForm: useSlurmDeployForm },
  Settings: SlurmSettingsPage,
  configurationSections: [SlurmHardwareCountersSection],
  facts: slurmPlatformFacts,
  specTopology: slurmSpecTopology,
}
