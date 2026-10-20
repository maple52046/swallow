import { kubernetesVersionAtLeast } from '@/domain/platform/gpuOperator'
import type {
  GPUAllocationMode,
  GPUOperatorRelease,
  GPUOperatorSettings,
  GPUResourceNaming,
  PlatformGPUOperator,
} from '@/domain/platform/types'

/**
 * Editable GPU Operator options shared by the deploy wizard's Features step and the Platform's
 * GPU Operator dialog. `version` and `draDriverVersion` are empty until the operator picks one,
 * which means the catalog default. There is no node choice: the operator manages every node its
 * Node Feature Discovery marks as an AMD GPU node (decision 062).
 */
export interface GPUOperatorFormValue {
  enabled: boolean
  version: string
  allocationMode: GPUAllocationMode
  draDriverVersion: string
  resourceNaming: GPUResourceNaming
  nodeLabeller: boolean
  metricsExporter: boolean
}

/** The documented defaults: Device Plugin, single naming, labeller on, exporter off. */
export const DEFAULT_GPU_OPERATOR_FORM: GPUOperatorFormValue = {
  enabled: false,
  version: '',
  allocationMode: 'device-plugin',
  draDriverVersion: '',
  resourceNaming: 'single',
  nodeLabeller: true,
  metricsExporter: false,
}

/**
 * Starts the dialog form from the Platform's recorded settings so a change edits what is there.
 * A Platform without a recorded operator starts from the defaults, enabled.
 */
export function gpuOperatorFormFromPlatform(operator: PlatformGPUOperator | null): GPUOperatorFormValue {
  if (!operator) return { ...DEFAULT_GPU_OPERATOR_FORM, enabled: true }
  return {
    enabled: true,
    version: operator.version,
    allocationMode: operator.allocationMode,
    draDriverVersion: operator.draDriverVersion ?? '',
    resourceNaming: operator.resourceNaming,
    nodeLabeller: operator.nodeLabeller,
    metricsExporter: operator.metricsExporter,
  }
}

/** The release the form resolves to: the chosen version, or the catalog default. */
export function resolveGPUOperatorRelease(
  value: GPUOperatorFormValue,
  releases: readonly GPUOperatorRelease[],
): GPUOperatorRelease | undefined {
  return releases.find((release) => (value.version ? release.version === value.version : release.default))
}

/**
 * Why the enabled options cannot be submitted yet, or null when they can. It checks what the
 * dashboard can know — an AMD GPU among the workload nodes, a loaded catalog, and the Kubernetes
 * floors for the release and for DRA — and leaves every other rule to the backend. An unreadable
 * k0s version is not a problem here; the backend rejects it with its own message.
 */
export function gpuOperatorFormProblem(
  value: GPUOperatorFormValue,
  hasAMDGPUWorkload: boolean,
  releases: readonly GPUOperatorRelease[] | null,
  k0sVersion: string | null,
): string | null {
  if (!value.enabled) return null
  if (!hasAMDGPUWorkload) return 'No workload Server in this Platform has an AMD compute GPU.'
  if (!releases) return 'GPU Operator releases are not loaded yet.'
  const release = resolveGPUOperatorRelease(value, releases)
  if (!release) return 'Choose a GPU Operator version.'
  if (k0sVersion && kubernetesVersionAtLeast(k0sVersion, release.minKubernetesVersion) === false) {
    return `GPU Operator ${release.version} needs Kubernetes ${release.minKubernetesVersion} or later.`
  }
  if (value.allocationMode === 'dra' && k0sVersion &&
    kubernetesVersionAtLeast(k0sVersion, release.draMinKubernetesVersion) === false) {
    return `DRA needs Kubernetes ${release.draMinKubernetesVersion} or later. Use the Device Plugin, or a newer k0s version.`
  }
  return null
}

/**
 * The request body the form produces (platforms.md "Settings"). Options the operator left at the
 * catalog default are omitted so the backend applies the default it documents; the DRA driver
 * version is sent only in DRA mode.
 */
export function gpuOperatorFormSettings(value: GPUOperatorFormValue): GPUOperatorSettings {
  if (!value.enabled) return { enabled: false }
  return {
    enabled: true,
    ...(value.version ? { version: value.version } : {}),
    allocationMode: value.allocationMode,
    ...(value.allocationMode === 'dra' && value.draDriverVersion ? { draDriverVersion: value.draDriverVersion } : {}),
    resourceNaming: value.resourceNaming,
    nodeLabeller: value.nodeLabeller,
    metricsExporter: value.metricsExporter,
  }
}
