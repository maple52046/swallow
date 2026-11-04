import { useMemo, useState } from 'react'
import { GPU_ALLOCATION_MODE_LABELS, GPU_RESOURCE_NAMING_LABELS, hasAMDComputeGPU } from '@/domain/platform/gpuOperator'
import type { DeployReviewItem, PlatformDeployFeature, PlatformDeployFeatureContext } from '@/modules/types'
import { GPUOperatorDeployFeature } from './GPUOperatorDeployFeature'
import {
  DEFAULT_GPU_OPERATOR_FORM,
  gpuOperatorFormProblem,
  gpuOperatorFormSettings,
  resolveGPUOperatorRelease,
  type GPUOperatorFormValue,
} from './gpuOperatorForm'
import { useGPUOperatorReleases } from './useGPUOperatorReleases'

/**
 * The wizard's GPU Operator choices. `touched` turns true at the user's first change of the card,
 * its switch or any option; until then `value.enabled` is ignored and the operator follows the
 * selected Servers' hardware.
 */
interface GPUOperatorDeployState {
  value: GPUOperatorFormValue
  touched: boolean
}

const UNTOUCHED: GPUOperatorDeployState = { value: DEFAULT_GPU_OPERATOR_FORM, touched: false }

/**
 * The review lines of the operator: what a deploy installs, or that it installs nothing. The DRA
 * driver and resource naming each apply to one allocation mode, so only that mode's line is listed.
 */
function gpuOperatorReviewItems(form: GPUOperatorFormValue, releaseVersion: string | undefined, draDriverVersion: string | undefined): DeployReviewItem[] {
  if (!form.enabled) return [{ label: 'AMD GPU Operator', value: 'Not installed' }]
  return [
    { label: 'AMD GPU Operator', value: `${releaseVersion ?? 'Default release'} · ${GPU_ALLOCATION_MODE_LABELS[form.allocationMode]}` },
    form.allocationMode === 'dra'
      ? { label: 'DRA driver', value: form.draDriverVersion || draDriverVersion || 'Release default' }
      : { label: 'Resource naming', value: GPU_RESOURCE_NAMING_LABELS[form.resourceNaming] },
    { label: 'Node Labeller', value: form.nodeLabeller ? 'On' : 'Off' },
    { label: 'GPU monitoring', value: form.metricsExporter ? 'Device Metrics Exporter, collected by the Site Prometheus' : 'Off' },
  ]
}

/**
 * The AMD GPU Operator as a Kubernetes deploy feature (decision 062). It can be turned on only when a
 * selected workload Server's inventory has an AMD compute GPU; the operator itself finds the nodes it
 * manages. The release catalog is fetched once the feature is on, and the chosen release must
 * support the Platform's Kubernetes version. The backend derives gpuStackOwner from it, so the
 * wizard never asks for that policy.
 *
 * Until the user touches the card the operator is proposed, with its Device Metrics Exporter
 * (DEFAULT_GPU_OPERATOR_FORM), exactly while a selected workload Server has an AMD compute GPU, so
 * changing the workers turns the proposal on or off. Once touched the user's choice stands: an
 * explicit off is never turned back on, and an explicit on whose AMD workers are gone blocks the
 * step with gpuOperatorFormProblem's reason rather than submitting what the backend refuses.
 * `reset` returns to the untouched proposal. The request carries `gpuOperator` only while it is on,
 * since omitting it is what installs no operator.
 */
export function useGPUOperatorDeployFeature({ servers, workloadServerIds, platformVersion }: PlatformDeployFeatureContext): PlatformDeployFeature {
  const [state, setState] = useState<GPUOperatorDeployState>(UNTOUCHED)
  const hasAMDGPUWorkload = useMemo(
    () => workloadServerIds.some((serverId) => {
      const server = servers.find((candidate) => candidate.id === serverId)
      return Boolean(server && hasAMDComputeGPU(server.status.gpus))
    }),
    [servers, workloadServerIds],
  )
  const form = state.touched ? state.value : { ...state.value, enabled: hasAMDGPUWorkload }
  const { state: releases, reload } = useGPUOperatorReleases(form.enabled)
  const catalog = releases.status === 'ready' ? releases.releases : null
  const problem = gpuOperatorFormProblem(form, hasAMDGPUWorkload, catalog, platformVersion?.trim() ?? '')
  const release = form.enabled && catalog ? resolveGPUOperatorRelease(form, catalog) : undefined
  return {
    content: (
      <GPUOperatorDeployFeature
        value={form}
        onChange={(value) => setState({ value, touched: true })}
        hasAMDGPUWorkload={hasAMDGPUWorkload}
        releases={releases}
        onRetryReleases={reload}
        k0sVersion={platformVersion?.trim() || null}
      />
    ),
    valid: problem === null,
    blockedReason: problem ?? undefined,
    reviewItems: gpuOperatorReviewItems(form, release?.version, release?.defaultDraDriverVersion),
    input: form.enabled ? { gpuOperator: gpuOperatorFormSettings(form) } : {},
    reset: () => setState(UNTOUCHED),
  }
}
