import { useMemo } from 'react'
import type { ChangePlan } from '@/domain/convergence/changePlan'
import { usePlatformNamesById } from '@/presentation/hooks/usePlatformNames'
import { useServerNames } from '@/presentation/hooks/useServerNames'
import { changePlanResourceIds, type ChangePlanNames } from './changePlanPresentation'

/**
 * Resolves the names of every Platform and Server the listed Change Plans refer to, for the Change
 * Plans page (the contract gives only `{kind, id}`).
 *
 * The lookups are batched for the whole page rather than per card or row: one Platform list read
 * across all Sites (plans are not Site-scoped) and one read per distinct Server id, each repeated
 * only when a new id appears, so the page's polling while a plan is approved sends no extra reads.
 * Until a name arrives, or when a resource no longer exists, the page shows its id.
 */
export function useChangePlanNames(plans: readonly ChangePlan[]): ChangePlanNames {
  const platformIds = useMemo(() => changePlanResourceIds(plans, 'platform'), [plans])
  const serverIds = useMemo(() => changePlanResourceIds(plans, 'server'), [plans])
  const platforms = usePlatformNamesById(platformIds)
  const servers = useServerNames(serverIds)
  return useMemo(() => ({ platforms, servers }), [platforms, servers])
}
