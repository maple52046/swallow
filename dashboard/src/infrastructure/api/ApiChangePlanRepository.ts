import type { ChangePlanFilter, ChangePlanRepository } from '@/application/ports/ChangePlanRepository'
import type { ChangePlan } from '@/domain/convergence/changePlan'
import { apiRequest } from './client'

/**
 * The `{ items: [...] }` envelope the plan list returns. Exported only so `contractCompatibility.ts`
 * can prove the list response reads as it.
 */
export interface ChangePlanList {
  items: ChangePlan[]
}

/**
 * Maps the Active Change Plans contract (`/api/v1/change-plans`, `change-plans.md`) onto the
 * ChangePlanRepository port. The contract's JSON already matches the domain shape, so responses
 * are returned directly; `state` filters repeat as query parameters. No secret crosses this
 * boundary.
 */
export class ApiChangePlanRepository implements ChangePlanRepository {
  async listChangePlans(filter?: ChangePlanFilter): Promise<ChangePlan[]> {
    const params = new URLSearchParams()
    if (filter?.resourceKind) params.set('resourceKind', filter.resourceKind)
    if (filter?.resourceId) params.set('resourceId', filter.resourceId)
    for (const state of filter?.states ?? []) params.append('state', state)
    const query = params.toString()
    const response = await apiRequest<ChangePlanList>(`/api/v1/change-plans${query ? `?${query}` : ''}`)
    return response.items
  }

  getChangePlan(id: string): Promise<ChangePlan> {
    return apiRequest<ChangePlan>(`/api/v1/change-plans/${encodeURIComponent(id)}`)
  }

  approveChangePlan(id: string): Promise<ChangePlan> {
    return apiRequest<ChangePlan>(`/api/v1/change-plans/${encodeURIComponent(id)}/approve`, { method: 'POST' })
  }

  rejectChangePlan(id: string): Promise<ChangePlan> {
    return apiRequest<ChangePlan>(`/api/v1/change-plans/${encodeURIComponent(id)}/reject`, { method: 'POST' })
  }
}
