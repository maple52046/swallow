import type { HardwareCounterReason, PlatformOmnistat } from '@/domain/platform/types'
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
 * route's per-Server breakdown, or nothing when the deployment did not ask for them or Omnistat was
 * uninstalled. The nodes are the record of the install that last ran; while it has not finished, or
 * when it failed before reporting, the summary says so instead of showing an empty list. A reason the
 * contract adds later is shown as its raw value rather than hidden.
 */
export function hardwareCounterStates(omnistat: PlatformOmnistat): PlatformMonitoringServerStates | undefined {
  const counters = omnistat.hardwareCounters
  if (!counters.requested || omnistat.state === 'absent') return undefined
  const collecting = counters.nodes.filter((node) => node.state === 'collecting').length
  let summary: string
  if (counters.nodes.length > 0) {
    summary = `VALUBusy is collected on ${collecting} of ${counters.nodes.length} compute nodes. A node that cannot collect it keeps its other metrics.`
  } else if (omnistat.state === 'installing') {
    summary = 'Each compute node reports whether it collects them when the Omnistat install finishes.'
  } else if (omnistat.state === 'failed') {
    summary = 'The Omnistat install did not finish, so no compute node reported them.'
  } else {
    summary = 'No compute node reported them.'
  }
  return {
    title: 'GPU hardware counters (VALUBusy)',
    summary,
    servers: counters.nodes.map((node) => ({
      serverId: node.serverId,
      state: node.state,
      statusLabel: node.state === 'collecting' ? 'Collecting' : 'Unavailable',
      ...(node.reason ? { reason: REASON_TEXT[node.reason] ?? node.reason } : {}),
    })),
  }
}
