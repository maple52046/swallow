import type { ChangePlanFilter, ChangePlanRepository } from '@/application/ports/ChangePlanRepository'
import type { ChangePlan } from '@/domain/convergence/changePlan'
import { apiRequest } from './client'
import type { JsonResponse } from './contractTypes'
import type { operations } from './generated/openapi'

/**
 * Maps the Active Change Plans contract (`/api/v1/change-plans`, `change-plans.md`) onto the
 * ChangePlanRepository port. The contract's JSON already matches the domain shape, which the
 * generated response types prove where each is returned; `state` filters repeat as query
 * parameters. No secret crosses this boundary.
 */
export class ApiChangePlanRepository implements ChangePlanRepository {
  async listChangePlans(filter?: ChangePlanFilter): Promise<ChangePlan[]> {
    const params = new URLSearchParams()
    if (filter?.resourceKind) params.set('resourceKind', filter.resourceKind)
    if (filter?.resourceId) params.set('resourceId', filter.resourceId)
    for (const state of filter?.states ?? []) params.append('state', state)
    const query = params.toString()
    const response = await apiRequest<JsonResponse<operations['listChangePlans'], 200>>(
      `/api/v1/change-plans${query ? `?${query}` : ''}`,
    )
    return response.items
  }

  getChangePlan(id: string): Promise<ChangePlan> {
    return apiRequest<JsonResponse<operations['getChangePlan'], 200>>(`/api/v1/change-plans/${encodeURIComponent(id)}`)
  }

  approveChangePlan(id: string): Promise<ChangePlan> {
    return apiRequest<JsonResponse<operations['approveChangePlan'], 200>>(
      `/api/v1/change-plans/${encodeURIComponent(id)}/approve`, { method: 'POST' },
    )
  }

  rejectChangePlan(id: string): Promise<ChangePlan> {
    return apiRequest<JsonResponse<operations['rejectChangePlan'], 200>>(
      `/api/v1/change-plans/${encodeURIComponent(id)}/reject`, { method: 'POST' },
    )
  }
}
