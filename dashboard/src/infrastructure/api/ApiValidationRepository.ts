import type { Paginated } from '@/application/ports/ServerRepository'
import type { ValidationRepository, ValidationRunFilter } from '@/application/ports/ValidationRepository'
import type {
  UserSuiteInput,
  ValidationRun,
  ValidationRunInput,
  ValidationRunSummary,
  ValidationSchedule,
  ValidationScheduleInput,
  ValidationSuite,
} from '@/domain/validation/types'
import { ApiRequestError, apiRequest } from './client'
import type { JsonRequestBody, JsonResponse } from './contractTypes'
import type { operations } from './generated/openapi'

/** Answers a read of one object with null when the API reports that it does not exist. */
async function orNull<T>(read: Promise<T>): Promise<T | null> {
  try {
    return await read
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 404) return null
    throw error
  }
}

/**
 * Maps the Active validation contract (`/api/v1/validation/*`, `validation.md`) onto the
 * ValidationRepository port. The contract's JSON already has the domain's shape, which the
 * generated response types prove where each body is returned, and request bodies are typed by the
 * contract's request schemas. Run list filters are query parameters; ids are path-encoded. A user
 * suite's script crosses this boundary as written: the contract stores and returns it, and it must
 * hold no secret.
 */
export class ApiValidationRepository implements ValidationRepository {
  async listSuites(includeRetired = false): Promise<ValidationSuite[]> {
    const response = await apiRequest<JsonResponse<operations['listValidationSuites'], 200>>(
      `/api/v1/validation/suites${includeRetired ? '?includeRetired=true' : ''}`,
    )
    return response.items
  }

  getSuite(id: string): Promise<ValidationSuite | null> {
    return orNull(apiRequest<JsonResponse<operations['getValidationSuite'], 200>>(`/api/v1/validation/suites/${encodeURIComponent(id)}`))
  }

  createSuite(input: UserSuiteInput): Promise<ValidationSuite> {
    return apiRequest<JsonResponse<operations['createValidationSuite'], 201>>('/api/v1/validation/suites', {
      method: 'POST', body: JSON.stringify(userSuiteBody(input)),
    })
  }

  replaceSuite(id: string, input: UserSuiteInput): Promise<ValidationSuite> {
    return apiRequest<JsonResponse<operations['replaceValidationSuite'], 200>>(`/api/v1/validation/suites/${encodeURIComponent(id)}`, {
      method: 'PUT', body: JSON.stringify(userSuiteBody(input)),
    })
  }

  async retireSuite(id: string): Promise<void> {
    await apiRequest<void>(`/api/v1/validation/suites/${encodeURIComponent(id)}`, { method: 'DELETE' })
  }

  listRuns(filter: ValidationRunFilter = {}): Promise<Paginated<ValidationRunSummary>> {
    const params = new URLSearchParams()
    for (const key of ['siteId', 'suiteId', 'serverId', 'platformId', 'scheduleId', 'status', 'verdict', 'trigger'] as const) {
      const value = filter[key]
      if (value) params.set(key, value)
    }
    if (filter.active) params.set('active', 'true')
    if (filter.page) params.set('page', String(filter.page))
    if (filter.pageSize) params.set('pageSize', String(filter.pageSize))
    const query = params.toString()
    return apiRequest<JsonResponse<operations['listValidationRuns'], 200>>(`/api/v1/validation/runs${query ? `?${query}` : ''}`)
  }

  getRun(id: string): Promise<ValidationRun | null> {
    return orNull(apiRequest<JsonResponse<operations['getValidationRun'], 200>>(`/api/v1/validation/runs/${encodeURIComponent(id)}`))
  }

  startRun(input: ValidationRunInput): Promise<ValidationRun> {
    const body: JsonRequestBody<operations['startValidationRun']> = {
      suiteId: input.suiteId, serverIds: input.serverIds, platformId: input.platformId,
      parameters: input.parameters, confirm: input.confirm ?? false,
    }
    return apiRequest<JsonResponse<operations['startValidationRun'], 202>>('/api/v1/validation/runs', {
      method: 'POST', body: JSON.stringify(body),
    })
  }

  cancelRun(id: string): Promise<ValidationRun> {
    return apiRequest<JsonResponse<operations['cancelValidationRun'], 202>>(
      `/api/v1/validation/runs/${encodeURIComponent(id)}/cancel`, { method: 'POST' },
    )
  }

  async listSchedules(siteId?: string): Promise<ValidationSchedule[]> {
    const query = siteId ? `?siteId=${encodeURIComponent(siteId)}` : ''
    const response = await apiRequest<JsonResponse<operations['listValidationSchedules'], 200>>(`/api/v1/validation/schedules${query}`)
    return response.items
  }

  createSchedule(input: ValidationScheduleInput): Promise<ValidationSchedule> {
    return apiRequest<JsonResponse<operations['createValidationSchedule'], 201>>('/api/v1/validation/schedules', {
      method: 'POST', body: JSON.stringify(scheduleBody(input)),
    })
  }

  replaceSchedule(id: string, input: ValidationScheduleInput): Promise<ValidationSchedule> {
    return apiRequest<JsonResponse<operations['replaceValidationSchedule'], 200>>(
      `/api/v1/validation/schedules/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(scheduleBody(input)) },
    )
  }

  async deleteSchedule(id: string): Promise<void> {
    await apiRequest<void>(`/api/v1/validation/schedules/${encodeURIComponent(id)}`, { method: 'DELETE' })
  }
}

/** The contract's user suite body; an omitted field takes the API's default. */
function userSuiteBody(input: UserSuiteInput): Partial<JsonRequestBody<operations['createValidationSuite']>> {
  return {
    name: input.name, description: input.description, script: input.script,
    runAs: input.runAs, timeoutSeconds: input.timeoutSeconds, impact: input.impact,
  }
}

/** The contract's schedule body; a cron trigger alone carries its expression and time zone. */
function scheduleBody(input: ValidationScheduleInput): JsonRequestBody<operations['createValidationSchedule']> {
  return {
    name: input.name, suiteId: input.suiteId, serverIds: input.serverIds, platformId: input.platformId,
    parameters: input.parameters, enabled: input.enabled, generation: input.generation,
    trigger: input.trigger.kind === 'cron'
      ? { kind: 'cron', cron: input.trigger.cron, timezone: input.trigger.timezone || undefined }
      : { kind: 'after-deployment' },
  }
}
