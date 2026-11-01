import type { AcceleratorModuleUI } from '@/modules/types'
import { PlatformGPUOperatorSection } from './PlatformGPUOperatorSection'

/** The AMD Accelerator module's UI: the AMD GPU Operator section of Kubernetes Platforms. */
export const amdModule: AcceleratorModuleUI = {
  kind: 'accelerator',
  id: 'amd',
  label: 'AMD',
  platformSections: [{ platformType: 'kubernetes', Section: PlatformGPUOperatorSection }],
}
