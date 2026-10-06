/**
 * Resolves a server's effective exporter owner for display, mirroring the backend rule.
 *
 * Ownership decides which subsystem installs a host's exporters, and there is exactly one
 * owner per host so two exporters never contend for a fixed port. The value shown here is
 * derived, not stored, and must match how the backend resolves it (see the platform
 * `exporterOwner` policy and docs/decisions/003-metrics-label-contract.md).
 */
import type { Platform, ExporterOwner } from '@/domain/platform/types'
import { serverGPUsByKind } from '@/domain/server/gpu'
import type { Server } from '@/domain/server/types'

/** The effective owner shown per host: the platform policy values plus `unmanaged`. */
export type EffectiveExporterOwner = ExporterOwner | 'unmanaged'

/**
 * Resolves the effective owner in the same order as the backend: a locked machine is
 * `unmanaged` (swallow must not touch it) regardless of any platform; otherwise a platform
 * member follows its platform's policy; otherwise `ansible`. A membership pointing at a
 * platform not present in `platformById` falls back to `ansible`, matching a stale membership.
 */
export function resolveExporterOwner(
  server: Server,
  platformById: Map<string, Platform>,
): EffectiveExporterOwner {
  if (server.provisioning?.locked) {
    return 'unmanaged'
  }
  const platformId = server.membership?.platformId
  if (platformId) {
    const platform = platformById.get(platformId)
    if (platform) {
      return platform.exporterOwner
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
  if (serverGPUsByKind(server, 'compute').length > 0) {
    return true
  }
  return server.tags.some((tag) => tag === 'amd-gpu' || tag === 'nvidia-gpu')
}
