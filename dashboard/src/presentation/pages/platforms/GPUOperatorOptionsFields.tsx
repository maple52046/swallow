import { Button, Field, Stack, Switch, Text } from '@chakra-ui/react'
import { GPU_ALLOCATION_MODE_LABELS, GPU_RESOURCE_NAMING_LABELS } from '@/domain/platform/gpuOperator'
import type { GPUAllocationMode, GPUResourceNaming } from '@/domain/platform/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Select } from '@/presentation/components/ui/select'
import {
  gpuOperatorFormProblem,
  resolveGPUOperatorRelease,
  type GPUOperatorFormValue,
} from './gpuOperatorForm'
import type { GPUOperatorReleasesState } from './useGPUOperatorReleases'

interface GPUOperatorOptionsFieldsProps {
  value: GPUOperatorFormValue
  onChange: (next: GPUOperatorFormValue) => void
  /**
   * Whether any workload Server has an AMD compute GPU in its inventory. It only decides whether
   * the operator can be enabled; the operator itself finds the nodes it manages.
   */
  hasAMDGPUWorkload: boolean
  releases: GPUOperatorReleasesState
  onRetryReleases: () => void
  /** The Platform's k0s version, used only to warn about Kubernetes floors; null when unknown. */
  k0sVersion: string | null
  /**
   * The deploy wizard shows the "Install AMD GPU Operator" checkbox and expands the options only
   * when it is checked. The Platform dialog edits an operator being installed or changed, so it
   * hides the checkbox and always shows the options.
   */
  showInstallToggle: boolean
  /** Prefixes control ids so two forms never share a DOM id. */
  idPrefix: string
}

/**
 * The AMD GPU Operator options (decision 062), shared by the deploy wizard's Features step and the
 * Platform's GPU Operator dialog so both offer the same choices with the same defaults.
 *
 * Choices come from the release catalog; until it loads the options stay visible but the form
 * reports a problem, so a version is never guessed. DRA-only and Device-Plugin-only choices appear
 * only in their mode. There is deliberately no node list (decision 062): the form states that the
 * operator's Node Feature Discovery decides. When no workload Server has an AMD GPU the install
 * checkbox is disabled and the reason is shown as text. Every problem is announced in an alert
 * rather than by colour alone.
 */
export function GPUOperatorOptionsFields({
  value,
  onChange,
  hasAMDGPUWorkload,
  releases,
  onRetryReleases,
  k0sVersion,
  showInstallToggle,
  idPrefix,
}: GPUOperatorOptionsFieldsProps) {
  const catalog = releases.status === 'ready' ? releases.releases : null
  const release = catalog ? resolveGPUOperatorRelease(value, catalog) : undefined
  const problem = gpuOperatorFormProblem(value, hasAMDGPUWorkload, catalog, k0sVersion)
  const expanded = !showInstallToggle || value.enabled
  const update = (patch: Partial<GPUOperatorFormValue>) => onChange({ ...value, ...patch })

  return (
    <Stack gap="4" className="sw-gpu-operator-options">
      {showInstallToggle && (
        <Field.Root>
          <Checkbox
            id={`${idPrefix}-install`}
            checked={value.enabled}
            disabled={!hasAMDGPUWorkload && !value.enabled}
            onCheckedChange={(checked) => update({ enabled: checked })}
          >
            Install AMD GPU Operator
          </Checkbox>
          <Field.HelperText>
            {hasAMDGPUWorkload
              ? "Makes this Platform's AMD GPUs schedulable in Kubernetes, using the GPU driver in the OS image."
              : 'None of the selected workload Servers has an AMD compute GPU.'}
          </Field.HelperText>
        </Field.Root>
      )}

      {expanded && (
        <>
          {releases.status === 'error' && (
            <Alert status="error" title="GPU Operator releases are unavailable">
              {releases.message}
              <Button size="sm" variant="outline" onClick={onRetryReleases}>Retry</Button>
            </Alert>
          )}
          <div className="sw-form-grid">
            <Field.Root required>
              <Field.Label>Allocation mode</Field.Label>
              <Select
                id={`${idPrefix}-mode`}
                aria-label="GPU allocation mode"
                value={value.allocationMode}
                onChange={(next) => update({ allocationMode: next as GPUAllocationMode })}
                options={(['device-plugin', 'dra'] as const).map((mode) => ({ value: mode, label: GPU_ALLOCATION_MODE_LABELS[mode] }))}
              />
              <Field.HelperText>
                {value.allocationMode === 'dra'
                  ? 'Pods claim GPUs through a ResourceClaim of the gpu.amd.com device class. Needs Kubernetes 1.34 or later.'
                  : 'Pods request GPUs as the amd.com/gpu resource in resources.limits.'}
              </Field.HelperText>
            </Field.Root>
            <Field.Root required>
              <Field.Label>GPU Operator version</Field.Label>
              <Select
                id={`${idPrefix}-version`}
                aria-label="GPU Operator version"
                value={release?.version ?? ''}
                placeholder={releases.status === 'loading' ? 'Loading releases' : 'Choose a version'}
                disabled={!catalog}
                onChange={(next) => update({ version: next, draDriverVersion: '' })}
                options={(catalog ?? []).map((item) => ({
                  value: item.version,
                  label: item.default ? `${item.version} (verified default)` : item.version,
                }))}
              />
            </Field.Root>
            {value.allocationMode === 'dra' && (
              <Field.Root required>
                <Field.Label>DRA driver version</Field.Label>
                <Select
                  id={`${idPrefix}-dra-driver`}
                  aria-label="DRA driver version"
                  value={value.draDriverVersion || release?.defaultDraDriverVersion || ''}
                  disabled={!release}
                  onChange={(next) => update({ draDriverVersion: next })}
                  options={(release?.draDriverVersions ?? []).map((version) => ({ value: version, label: version }))}
                />
              </Field.Root>
            )}
            {value.allocationMode === 'device-plugin' && (
              <Field.Root>
                <Field.Label>Resource naming</Field.Label>
                <Select
                  id={`${idPrefix}-naming`}
                  aria-label="GPU resource naming"
                  value={value.resourceNaming}
                  onChange={(next) => update({ resourceNaming: next as GPUResourceNaming })}
                  options={(['single', 'mixed'] as const).map((naming) => ({ value: naming, label: GPU_RESOURCE_NAMING_LABELS[naming] }))}
                />
                <Field.HelperText>Mixed names partitioned GPUs by partition type instead of one amd.com/gpu resource.</Field.HelperText>
              </Field.Root>
            )}
            <Field.Root>
              <Switch.Root
                checked={value.nodeLabeller}
                colorPalette="brand"
                onCheckedChange={(details) => update({ nodeLabeller: details.checked === true })}
              >
                <Switch.HiddenInput id={`${idPrefix}-labeller`} role="switch" />
                <Switch.Control><Switch.Thumb /></Switch.Control>
                <Switch.Label>Node Labeller</Switch.Label>
              </Switch.Root>
              <Field.HelperText>Labels nodes with amd.com/gpu.* properties such as product name and VRAM.</Field.HelperText>
            </Field.Root>
            <Field.Root>
              <Switch.Root
                checked={value.metricsExporter}
                colorPalette="brand"
                onCheckedChange={(details) => update({ metricsExporter: details.checked === true })}
              >
                <Switch.HiddenInput id={`${idPrefix}-metrics`} role="switch" />
                <Switch.Control><Switch.Thumb /></Switch.Control>
                <Switch.Label>Device Metrics Exporter</Switch.Label>
              </Switch.Root>
              <Field.HelperText>Runs AMD's Device Metrics Exporter on the pod network. Swallow's GPU charts keep using the host exporter.</Field.HelperText>
            </Field.Root>
          </div>

          <Text className="sw-muted">
            The operator manages every node its Node Feature Discovery finds with a supported AMD GPU, physical or SR-IOV
            virtual function. Nodes added later are included the next time the operator is changed.
          </Text>

          {problem && releases.status !== 'error' && (
            <Alert status="warning" title="GPU Operator options are incomplete">
              {problem}
            </Alert>
          )}
        </>
      )}
    </Stack>
  )
}
