import { useMemo, useState } from 'react'
import { GPU_ALLOCATION_MODE_LABELS, hasAMDComputeGPU } from '@/domain/platform/gpuOperator'
import type { PlatformDeployFeature, PlatformDeployFeatureContext } from '@/modules/types'
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
 * The AMD GPU Operator as a Kubernetes deploy feature (decision 062). It can be turned on only when a
 * selected workload Server's inventory has an AMD compute GPU; the operator itself finds the nodes it
 * manages. The release catalog is fetched once the feature is chosen, and the chosen release must
 * support the Platform's Kubernetes version. The backend derives gpuStackOwner from it, so the
 * wizard never asks for that policy.
 */
export function useGPUOperatorDeployFeature({ servers, workloadServerIds, platformVersion }: PlatformDeployFeatureContext): PlatformDeployFeature {
  const [form, setForm] = useState<GPUOperatorFormValue>(DEFAULT_GPU_OPERATOR_FORM)
  const hasAMDGPUWorkload = useMemo(
    () => workloadServerIds.some((serverId) => {
      const server = servers.find((candidate) => candidate.id === serverId)
      return Boolean(server && hasAMDComputeGPU(server.status.gpus))
    }),
    [servers, workloadServerIds],
  )
  const { state: releases, reload } = useGPUOperatorReleases(form.enabled)
  const catalog = releases.status === 'ready' ? releases.releases : null
  const problem = gpuOperatorFormProblem(form, hasAMDGPUWorkload, catalog, platformVersion?.trim() ?? '')
  const release = form.enabled && catalog ? resolveGPUOperatorRelease(form, catalog) : undefined
  const review = form.enabled ? `${release?.version ?? 'Default release'} · ${GPU_ALLOCATION_MODE_LABELS[form.allocationMode]}` : 'Not installed'
  return {
    content: (
      <GPUOperatorDeployFeature
        value={form}
        onChange={setForm}
        hasAMDGPUWorkload={hasAMDGPUWorkload}
        releases={releases}
        onRetryReleases={reload}
        k0sVersion={platformVersion?.trim() || null}
      />
    ),
    valid: problem === null,
    blockedReason: problem ?? undefined,
    reviewItems: [{ label: 'AMD GPU Operator', value: review }],
    input: form.enabled ? { gpuOperator: gpuOperatorFormSettings(form) } : {},
    reset: () => setForm(DEFAULT_GPU_OPERATOR_FORM),
  }
}
