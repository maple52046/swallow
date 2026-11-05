import { Boxes } from 'lucide-react'
import type { PlatformModuleUI } from '@/modules/types'
import { KubernetesApplicationsTab, KubernetesClusterTab, KubernetesNamespacesTab } from './KubernetesExplorer'
import { kubernetesPlatformFacts } from './kubernetesFacts'
import { KubernetesAccess } from './KubernetesAccess'
import { useKubernetesDeployForm } from './useKubernetesDeployForm'

/**
 * The live cluster explorer applies only to a swallow-deployed Kubernetes Platform with a recorded
 * credential (decision 032); a legacy registered record or one still deploying shows only Overview
 * and Activity, so its tabs never make a call that can only fail.
 */
const explorerAvailable = (platform: { origin: string; integrationId: string | null }) =>
  platform.origin === 'deployed' && Boolean(platform.integrationId)

/**
 * The Kubernetes module's UI: deploy form, compact access summary, live cluster routes,
 * configuration facts, and monitoring contributions.
 */
export const kubernetesModule: PlatformModuleUI = {
  kind: 'platform',
  id: 'kubernetes',
  label: 'Kubernetes',
  icon: Boxes,
  capabilities: ['Application workloads', 'Namespaces', 'Live cluster management'],
  badgePalette: 'blue',
  Access: KubernetesAccess,
  monitoringEntries: (platform) => platform.gpuOperator?.metricsExporter
    ? [{
        name: 'Kubernetes GPU metrics',
        state: platform.gpuOperator.state,
        detail: 'Device Metrics Exporter managed by AMD GPU Operator',
        workflowId: platform.gpuOperator.lastWorkflowId,
      }]
    : [],
  tabs: [
    { value: 'applications', label: 'Applications', available: explorerAvailable, Content: KubernetesApplicationsTab },
    { value: 'namespaces', label: 'Namespaces', available: explorerAvailable, Content: KubernetesNamespacesTab },
    { value: 'cluster', label: 'Cluster', available: explorerAvailable, Content: KubernetesClusterTab },
  ],
  deploy: { icon: Boxes, namePlaceholder: 'lab-k0s', useForm: useKubernetesDeployForm },
  facts: kubernetesPlatformFacts,
}
