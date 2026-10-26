import { Badge } from '@chakra-ui/react'
import type { GPUOperatorReleasesState } from './useGPUOperatorReleases'
import { FeatureToggleCard } from './FeatureToggleCard'
import { GPUOperatorOptionsFields } from './GPUOperatorOptionsFields'
import type { GPUOperatorFormValue } from './gpuOperatorForm'

interface KubernetesDeploymentFeaturesProps {
  value: GPUOperatorFormValue
  onChange: (value: GPUOperatorFormValue) => void
  hasAMDGPUWorkload: boolean
  releases: GPUOperatorReleasesState
  onRetryReleases: () => void
  k0sVersion: string | null
}
/**
 * Kubernetes-only optional capabilities. The GPU Operator is a Platform feature rather than
 * Managed Software, so its enablement and settings stay together in one expandable card.
 */
export function KubernetesDeploymentFeatures({
  value,
  onChange,
  hasAMDGPUWorkload,
  releases,
  onRetryReleases,
  k0sVersion,
}: KubernetesDeploymentFeaturesProps) {
  return (
    <div className="sw-platform-feature-list">
      <FeatureToggleCard
        title="AMD GPU Operator"
        description="Make the selected workload nodes' AMD GPUs schedulable in Kubernetes by using the driver already provided by the OS image."
        checked={value.enabled}
        disabled={!hasAMDGPUWorkload && !value.enabled}
        disabledReason={!hasAMDGPUWorkload ? 'None of the selected workload Servers has an AMD compute GPU.' : undefined}
        onCheckedChange={(enabled) => onChange({ ...value, enabled })}
        switchLabel="Enable AMD GPU Operator"
        status={<Badge variant="subtle" colorPalette={value.enabled ? 'blue' : 'gray'}>{value.enabled ? 'Included' : 'Not included'}</Badge>}
      >
        <GPUOperatorOptionsFields
          value={value}
          onChange={onChange}
          hasAMDGPUWorkload={hasAMDGPUWorkload}
          releases={releases}
          onRetryReleases={onRetryReleases}
          k0sVersion={k0sVersion}
          showInstallToggle={false}
          idPrefix="deploy-gpu-operator"
        />
      </FeatureToggleCard>
    </div>
  )
}
