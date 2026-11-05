import { gpuOperatorStateLabel, gpuOperatorStatus } from '@/domain/platform/gpuOperator'
import type { AcceleratorModuleUI } from '@/modules/types'
import { PlatformGPUOperatorSection } from './PlatformGPUOperatorSection'
import { useGPUOperatorDeployFeature } from './useGPUOperatorDeployFeature'

/**
 * The AMD Accelerator module's UI: Kubernetes Configuration and Monitoring contributions plus
 * the GPU Operator option of a Kubernetes deploy.
 */
export const amdModule: AcceleratorModuleUI = {
  kind: 'accelerator',
  id: 'amd',
  label: 'AMD',
  configurationSections: [{ platformType: 'kubernetes', Section: PlatformGPUOperatorSection }],
  monitoringEntries: [{
    platformType: 'kubernetes',
    entries: (platform) => {
      const operator = platform.gpuOperator
      if (!operator || operator.state === 'absent') return []
      const included = operator.metricsExporter
      return [{
        name: 'AMD Device Metrics Exporter',
        state: included ? gpuOperatorStatus(operator.state) : 'archived',
        statusLabel: included ? gpuOperatorStateLabel(operator.state) : 'Not included',
        detail: included
          ? 'Managed by the AMD GPU Operator · Collected by the Site Prometheus at port 5001'
          : 'Not enabled in the AMD GPU Operator configuration.',
        workflowId: operator.lastWorkflowId,
      }]
    },
  }],
  deployFeatures: [{ platformType: 'kubernetes', useFeature: useGPUOperatorDeployFeature }],
}
