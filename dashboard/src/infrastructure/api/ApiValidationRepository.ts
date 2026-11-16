import type { Paginated } from '@/application/ports/ServerRepository'
import type { ValidationRepository, ValidationRunFilter } from '@/application/ports/ValidationRepository'
import {
  ValidationConflictError,
  type UserSuiteInput,
  type ValidationRun,
  type ValidationRunInput,
  type ValidationRunSummary,
  type ValidationSchedule,
  type ValidationScheduleInput,
  type ValidationSuite,
  type ValidationSuiteRevision,
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
 * Raises a `409` of a write as `ValidationConflictError`, keeping the API's message and request id,
 * so a screen can tell "re-read and decide" from any other refusal without reading status codes.
 */
async function conflictAware<T>(write: Promise<T>): Promise<T> {
  try {
    return await write
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 409) throw new ValidationConflictError(error.message, error.requestId)
    throw error
  }
}

/**
 * Maps the Active validation contract (`/api/v1/validation/*`, `validation.md`) onto the
 * ValidationRepository port. The contract's JSON already has the domain's shape, which the
 * generated response types prove where each body is returned, and request bodies are typed by the
 * contract's request schemas. Run list filters are query parameters; ids are path-encoded. A user
 * suite's script crosses this boundary as written: the contract stores and returns it to every
 * User, and it must hold no secret. A `409` from a suite replace or a run start becomes
 * `ValidationConflictError`; every other failure stays the client's `ApiRequestError`.
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

  getSuiteRevision(suiteId: string, revision: number): Promise<ValidationSuiteRevision | null> {
    return orNull(apiRequest<JsonResponse<operations['getValidationSuiteRevision'], 200>>(
      `/api/v1/validation/suites/${encodeURIComponent(suiteId)}/revisions/${encodeURIComponent(String(revision))}`,
    ))
  }

  createSuite(input: UserSuiteInput): Promise<ValidationSuite> {
    return apiRequest<JsonResponse<operations['createValidationSuite'], 201>>('/api/v1/validation/suites', {
      method: 'POST', body: JSON.stringify(userSuiteBody({ ...input, revision: undefined })),
    })
  }

  replaceSuite(id: string, input: UserSuiteInput): Promise<ValidationSuite> {
    return conflictAware(apiRequest<JsonResponse<operations['replaceValidationSuite'], 200>>(`/api/v1/validation/suites/${encodeURIComponent(id)}`, {
      method: 'PUT', body: JSON.stringify(userSuiteBody(input)),
    }))
  }

  async retireSuite(id: string): Promise<void> {
    await apiRequest<void>(`/api/v1/validation/suites/${encodeURIComponent(id)}`, { method: 'DELETE' })
  }

  listRuns(filter: ValidationRunFilter = {}): Promise<Paginated<ValidationRunSummary>> {
    const params = new URLSearchParams()
    for (const key of ['siteId', 'suiteId', 'serverId', 'platformId', 'scheduleId', 'status', 'verdict', 'trigger', 'requestedBy'] as const) {
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
      parameters: input.parameters, confirm: input.confirm ?? false, suiteRevision: input.suiteRevision,
    }
    return conflictAware(apiRequest<JsonResponse<operations['startValidationRun'], 202>>('/api/v1/validation/runs', {
      method: 'POST', body: JSON.stringify(body),
    }))
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

/**
 * The contract's user suite body; an omitted field takes the API's default. `revision` is the
 * replace precondition and is left out of a create, where the API ignores it anyway.
 */
function userSuiteBody(input: UserSuiteInput): Partial<JsonRequestBody<operations['createValidationSuite']>> {
  return {
    name: input.name, description: input.description, script: input.script,
    sudo: input.sudo, starterLevel: input.starterLevel, timeoutSeconds: input.timeoutSeconds, impact: input.impact,
    revision: input.revision,
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
