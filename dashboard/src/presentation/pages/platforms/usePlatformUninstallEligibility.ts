import { platformDeploymentServerIds, platformUninstallAvailability, platformUninstallDisabledReason } from '@/domain/platform/lifecycle'
import type { Platform } from '@/domain/platform/types'
import { useTargetLockProtection } from '@/presentation/hooks/useTargetLockProtection'

/** Which selected Platforms a bulk Uninstall would target and which it would skip, and why. */
export interface PlatformUninstallEligibility {
  eligible: Platform[]
  skipped: Array<{ platform: Platform; reason: string }>
  /** Set when the bulk Uninstall cannot run at all (still checking, or nothing eligible). */
  disabledReason?: string
}

/**
 * Splits a bulk selection into the Platforms an Uninstall can act on now and the ones it skips.
 *
 * Each Platform goes through the same rule as the detail page (`platformUninstallAvailability`):
 * its lifecycle first, then a live Server Lock read of its deployment targets. Only Platforms whose
 * lifecycle allows an uninstall are read, and the read runs while the selection is active so the
 * toolbar can disable Uninstall before a dialog opens. The read fails closed: while it is checking,
 * Uninstall is disabled; a Platform whose read failed is skipped. A new selection starts a new read
 * (`useTargetLockProtection` keys on the target set and ignores stale responses).
 */
export function usePlatformUninstallEligibility(platforms: readonly Platform[]): PlatformUninstallEligibility {
  const lifecycleEligible = platforms.filter((platform) => !platformUninstallDisabledReason(platform))
  const lock = useTargetLockProtection([...new Set(lifecycleEligible.flatMap(platformDeploymentServerIds))])
  const status = lock.checking ? 'checking' : lock.error ? 'error' : 'ready'
  const lockedNameById = new Map(lock.lockedTargets.map((target) => [target.serverId, target.name]))

  const eligible: Platform[] = []
  const skipped: PlatformUninstallEligibility['skipped'] = []
  for (const platform of platforms) {
    const lockedNames = platformDeploymentServerIds(platform).flatMap((serverId) => {
      const name = lockedNameById.get(serverId)
      return name ? [name] : []
    })
    const { reason } = platformUninstallAvailability(platform, { status, lockedNames })
    if (reason) skipped.push({ platform, reason })
    else eligible.push(platform)
  }

  const disabledReason = status === 'checking' && lifecycleEligible.length > 0
    ? 'Checking target protection.'
    : eligible.length === 0
      ? 'No selected platform can be uninstalled.'
      : undefined
  return { eligible, skipped, disabledReason }
}
