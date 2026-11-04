import { Boxes } from 'lucide-react'
import type { PlatformModuleUI } from '@/modules/types'
import { KubernetesApplicationsTab, KubernetesClusterTab, KubernetesNamespacesTab } from './KubernetesExplorer'
import { kubernetesPlatformFacts } from './kubernetesFacts'
import { KubernetesOverview } from './KubernetesOverview'
import { useKubernetesDeployForm } from './useKubernetesDeployForm'

/**
 * The live cluster explorer applies only to a swallow-deployed Kubernetes Platform with a recorded
 * credential (decision 032); a legacy registered record or one still deploying shows only Overview
 * and Activity, so its tabs never make a call that can only fail.
 */
const explorerAvailable = (platform: { origin: string; integrationId: string | null }) =>
  platform.origin === 'deployed' && Boolean(platform.integrationId)

/**
 * The Kubernetes module's UI: the k0s deploy form, the k0s Overview, the live cluster explorer tabs,
 * and the k0s facts of the Platform details card.
 */
export const kubernetesModule: PlatformModuleUI = {
  kind: 'platform',
  id: 'kubernetes',
  label: 'Kubernetes',
  badgePalette: 'blue',
  Overview: KubernetesOverview,
  tabs: [
    { value: 'applications', label: 'Applications', available: explorerAvailable, Content: KubernetesApplicationsTab },
    { value: 'namespaces', label: 'Namespaces', available: explorerAvailable, Content: KubernetesNamespacesTab },
    { value: 'cluster', label: 'Cluster', available: explorerAvailable, Content: KubernetesClusterTab },
  ],
  deploy: { icon: Boxes, namePlaceholder: 'lab-k0s', useForm: useKubernetesDeployForm },
  facts: kubernetesPlatformFacts,
}
