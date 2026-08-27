/**
 * Resolves a server's effective exporter owner for display, mirroring the backend rule.
 *
 * Ownership decides which subsystem installs a host's exporters, and there is exactly one
 * owner per host so two exporters never contend for a fixed port. The value shown here is
 * derived, not stored, and must match how the backend resolves it (see the cluster
 * `exporterOwner` policy and docs/decisions/003-metrics-label-contract.md).
 */
import type { Cluster, ExporterOwner } from '@/domain/cluster/types'
import type { Server } from '@/domain/server/types'

/** The effective owner shown per host: the cluster policy values plus `unmanaged`. */
export type EffectiveExporterOwner = ExporterOwner | 'unmanaged'

/**
 * Resolves the effective owner in the same order as the backend: a locked machine is
 * `unmanaged` (swallow must not touch it) regardless of any cluster; otherwise a cluster
 * member follows its cluster's policy; otherwise `ansible`. A membership pointing at a
 * cluster not present in `clusterById` falls back to `ansible`, matching a stale membership.
 */
export function resolveExporterOwner(
  server: Server,
  clusterById: Map<string, Cluster>,
): EffectiveExporterOwner {
  if (server.provisioning?.locked) {
    return 'unmanaged'
  }
  const clusterId = server.membership?.clusterId
  if (clusterId) {
    const cluster = clusterById.get(clusterId)
    if (cluster) {
      return cluster.exporterOwner
    }
  }
  return 'ansible'
}

/**
 * Whether a server should show GPU metrics: it has an AMD/NVIDIA GPU in inventory, or is
 * tagged as a GPU server (e.g. `amd-gpu`). Tag-based so a machine designated a GPU server
 * shows GPU cards even before its GPUs are detected.
 */
export function isGpuServer(server: Server): boolean {
  if (server.gpus.length > 0) {
    return true
  }
  return server.tags.some((tag) => tag === 'amd-gpu' || tag === 'nvidia-gpu')
}
