import type { ServerGPU } from '@/domain/server/types'
import type {
  GPUAllocationMode,
  GPUOperatorState,
  GPUResourceNaming,
} from './types'

/**
 * Whether a Server's GPU inventory has an AMD workload accelerator: a `compute` group whose
 * vendor names AMD, or whose model is the `1002:product` PCI id a provisioner reports when it
 * cannot name the device. It mirrors the backend rule for whether a Platform can enable the GPU
 * Operator, so the wizard offers the feature exactly when the deploy request will accept it; the
 * backend still decides, and the operator chooses the nodes it manages.
 */
export function hasAMDComputeGPU(gpus: readonly ServerGPU[]): boolean {
  return gpus.some((gpu) => {
    if (gpu.kind !== 'compute') return false
    const vendor = gpu.vendor.toLowerCase()
    return vendor.includes('advanced micro devices') || vendor.includes('amd') || gpu.model.trim().toLowerCase().startsWith('1002:')
  })
}

/**
 * Whether a k0s version (`v1.36.3+k0s.2`) is at least a catalog Kubernetes floor (`1.34`).
 * Returns null when either cannot be read, so the caller leaves the decision to the backend
 * instead of guessing.
 */
export function kubernetesVersionAtLeast(k0sVersion: string, minimum: string): boolean | null {
  const parse = (value: string): [number, number] | null => {
    const match = /^v?(\d+)\.(\d+)/.exec(value.trim())
    return match ? [Number(match[1]), Number(match[2])] : null
  }
  const version = parse(k0sVersion)
  const floor = parse(minimum)
  if (!version || !floor) return null
  return version[0] !== floor[0] ? version[0] > floor[0] : version[1] >= floor[1]
}

/** Operator-facing names for the GPU Allocation Mode domain values. */
export const GPU_ALLOCATION_MODE_LABELS: Record<GPUAllocationMode, string> = {
  'device-plugin': 'Device Plugin',
  dra: 'DRA',
}

/** Operator-facing names for the device plugin resource naming strategies. */
export const GPU_RESOURCE_NAMING_LABELS: Record<GPUResourceNaming, string> = {
  single: 'Single (amd.com/gpu)',
  mixed: 'Mixed (per partition type)',
}

const STATE_LABELS: Record<GPUOperatorState, string> = {
  installing: 'Installing',
  ready: 'Ready',
  failed: 'Failed',
  uninstalling: 'Uninstalling',
  absent: 'Not installed',
}

/** Operator-facing GPU Operator State label; `absent` reads as "Not installed". */
export function gpuOperatorStateLabel(state: GPUOperatorState): string {
  return STATE_LABELS[state]
}

/**
 * Whether a Workflow is still applying the latest change, so the state moves without operator
 * action. Drives the in-progress cue and polling on the Platform page.
 */
export function isGPUOperatorInProgress(state: GPUOperatorState): boolean {
  return state === 'installing' || state === 'uninstalling'
}

/** The shared StatusBadge family for a GPU Operator State. */
export function gpuOperatorStatus(state: GPUOperatorState): string {
  switch (state) {
    case 'ready':
      return 'active'
    case 'failed':
      return 'failed'
    case 'installing':
    case 'uninstalling':
      return 'running'
    default:
      return 'archived'
  }
}
