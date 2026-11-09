import { platformConvergesWithoutInput, settingObject } from '@/domain/platform/spec'
import type { HardwareCounterReason, Platform, PlatformSpec } from '@/domain/platform/types'
import type { PlatformMonitoringServerStates } from '@/modules/types'

/**
 * What each reason a compute node reports (glossary Omnistat Hardware Counters) means for the user,
 * phrased as the node's situation rather than as a failure: the node's other metrics are collected.
 */
const REASON_TEXT: Record<HardwareCounterReason, string> = {
  no_gpu: 'No AMD compute GPU on this node.',
  rocm_unsupported: 'Swallow ships no counter extension for this node\u2019s ROCm version.',
  glibc_too_old: 'This node\u2019s glibc is older than the counter extension needs.',
  artifact_unavailable: 'The counter extension could not be fetched from the Site\u2019s artifact source.',
  probe_failed: 'A test sample of the counters failed on this node\u2019s GPUs.',
  exporter_failed: 'The exporter did not start with the counters, so they were turned off again.',
}

/**
 * Projects a Slurm Platform's Omnistat hardware counters (platforms.md "Omnistat") into the Monitoring
 * route's per-Server breakdown under Omnistat, or nothing when the Platform has no swallow-managed
 * Omnistat whose spec keeps it on. It is shown whether or not the counters are asked for, because the
 * module's switch for them sits in it (the caller adds the Control). The summary is the switch
 * projection's: what the last install did with the counters, a change still running, or a spec it
 * has not carried out yet. The nodes are the record of the install that last ran, empty while one
 * runs or when the counters are off. A reason the contract adds later is shown as its raw value
 * rather than hidden.
 */
export function hardwareCounterStates(platform: Platform): PlatformMonitoringServerStates | undefined {
  const counters = hardwareCountersSwitch(platform)
  if (!counters || !platform.omnistat) return undefined
  return {
    title: 'GPU hardware counters (VALUBusy)',
    summary: counters.summary,
    servers: platform.omnistat.hardwareCounters.nodes.map((node) => ({
      serverId: node.serverId,
      state: node.state,
      statusLabel: node.state === 'collecting' ? 'Collecting' : 'Unavailable',
      ...(node.reason ? { reason: REASON_TEXT[node.reason] ?? node.reason } : {}),
    })),
  }
}

/**
 * Whether a Slurm spec asks Omnistat for the hardware counters (`settings.omnistat.hardwareCounters`,
 * platforms.md "Omnistat"). An absent setting is off, as in a deploy request.
 */
export function hardwareCountersWanted(spec: PlatformSpec | null): boolean {
  return settingObject(spec?.settings, 'omnistat')?.hardwareCounters === true
}

/**
 * What the hardware counters switch under Omnistat on the Monitoring route shows for a deployed Slurm
 * Platform (platforms.md "Omnistat"): `wanted` is the spec's setting, which the switch shows; `status`
 * and `summary` describe the Omnistat record, which follows the install that last ran.
 */
export interface HardwareCountersSwitch {
  wanted: boolean
  /** The badge: `value` picks its colour only, `label` carries the meaning. */
  status: { value: string; label: string }
  summary: string
  /** A Workflow is carrying a change out, so the page keeps polling and the switch waits. */
  inProgress: boolean
  /** Why the switch cannot be turned now, as the user reads it; null when it can. */
  blockedReason: string | null
}

/**
 * Projects a Slurm Platform into its hardware counters switch, or nothing when the Platform has no
 * swallow-managed Omnistat whose spec keeps it on: then there are no counters to switch (the Device
 * Metrics Exporter has none). Turning them on or off is a non-destructive spec write that a
 * `configure-omnistat` Workflow carries out while the Platform stays active; the record restarts as
 * `installing` with no nodes, so an install that runs is shown as a change in progress, and a spec
 * the record does not follow yet (a write the controller has not started, or refused) says so instead
 * of presenting the old outcome as the new one. Whether a change is allowed mirrors the backend's
 * preconditions only to explain a disabled switch; the API decides.
 */
export function hardwareCountersSwitch(platform: Platform): HardwareCountersSwitch | undefined {
  const omnistat = platform.omnistat
  if (!omnistat || omnistat.state === 'absent' || settingObject(platform.spec?.settings, 'omnistat')?.enabled !== true) {
    return undefined
  }
  const wanted = hardwareCountersWanted(platform.spec)
  const counters = omnistat.hardwareCounters
  const inProgress = omnistat.state === 'installing' || platformConvergesWithoutInput(platform)
  let status: HardwareCountersSwitch['status']
  let summary: string
  if (inProgress && platform.lifecycleState !== 'active') {
    status = { value: 'running', label: 'Installing' }
    summary = 'Each compute node reports whether it collects them when the Omnistat install finishes.'
  } else if (inProgress) {
    status = { value: 'running', label: wanted ? 'Turning on' : 'Turning off' }
    summary = wanted
      ? 'Omnistat is being reconfigured on every compute node; each node reports whether it collects them when the workflow finishes.'
      : 'Omnistat is being reconfigured on every compute node, which then runs it without the counters.'
  } else if (omnistat.state === 'failed') {
    status = { value: 'failed', label: 'Failed' }
    summary = 'The last Omnistat install did not finish, so its compute nodes may still run the previous setting. Open its workflow for the cause.'
  } else if (counters.requested !== wanted) {
    status = { value: 'warning', label: 'Not applied' }
    summary = `The spec turns them ${wanted ? 'on' : 'off'}, but Omnistat still runs with them ${counters.requested ? 'on' : 'off'}. The Platform's spec state says why.`
  } else if (wanted) {
    const collecting = counters.nodes.filter((node) => node.state === 'collecting').length
    status = { value: 'active', label: 'On' }
    summary = counters.nodes.length > 0
      ? `VALUBusy is collected on ${collecting} of ${counters.nodes.length} compute nodes. A node that cannot collect it keeps its other metrics.`
      : 'No compute node reported them.'
  } else {
    status = { value: 'offline', label: 'Off' }
    summary = 'Every compute node runs Omnistat without the counters.'
  }
  const blockedReason = platform.origin !== 'deployed'
    ? 'Only a Platform Swallow deployed can switch its hardware counters.'
    : platform.lifecycleState !== 'active'
      ? 'The hardware counters can be switched once the Platform is active.'
      : inProgress
        ? 'Wait for the running Omnistat change to finish.'
        : null
  return { wanted, status, summary, inProgress, blockedReason }
}
