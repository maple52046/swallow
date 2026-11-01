import type { AcceleratorModuleUI } from '@/modules/types'
import { PlatformGPUOperatorSection } from './PlatformGPUOperatorSection'
import { useGPUOperatorDeployFeature } from './useGPUOperatorDeployFeature'

/**
 * The AMD Accelerator module's UI: the AMD GPU Operator section of Kubernetes Platforms and the
 * GPU Operator option of a Kubernetes deploy.
 */
export const amdModule: AcceleratorModuleUI = {
  kind: 'accelerator',
  id: 'amd',
  label: 'AMD',
  platformSections: [{ platformType: 'kubernetes', Section: PlatformGPUOperatorSection }],
  deployFeatures: [{ platformType: 'kubernetes', useFeature: useGPUOperatorDeployFeature }],
}
