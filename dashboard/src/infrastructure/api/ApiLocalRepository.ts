import type {
  LocalRepositoryContentQuery,
  LocalRepositoryPort,
  LocalRepositorySpecWrite,
} from '@/application/ports/LocalRepositoryPort'
import {
  LocalRepositorySpecConflictError,
  type LocalRepository,
  type LocalRepositoryContentPage,
} from '@/domain/localRepository/types'
import { ApiRequestError, apiRequest, apiRequestText } from './client'
import type { JsonRequestBody, JsonResponse } from './contractTypes'
import type { operations } from './generated/openapi'

/**
 * Maps the Active Local Repository contract (`/api/v1/sites/{siteId}/local-repository`,
 * local-repository.md) onto the LocalRepositoryPort. The contract's JSON already matches the domain
 * shape, which the generated response types prove where each body is returned. Two answers are
 * translated rather than thrown as transport errors:
 * - `404` on the read is a Site without a `repository` Integration, a domain miss returned as `null`;
 * - `409` on the spec write is a stale `generation`, raised as {@link LocalRepositorySpecConflictError}.
 *
 * No credential crosses this boundary in either direction.
 */
export class ApiLocalRepository implements LocalRepositoryPort {
  async getLocalRepository(siteId: string): Promise<LocalRepository | null> {
    try {
      return await apiRequest<JsonResponse<operations['getLocalRepository'], 200>>(localRepositoryPath(siteId))
    } catch (error) {
      if (error instanceof ApiRequestError && error.status === 404) return null
      throw error
    }
  }

  browseContent(siteId: string, query: LocalRepositoryContentQuery): Promise<LocalRepositoryContentPage> {
    const params = new URLSearchParams({ format: query.format, page: String(query.page), pageSize: String(query.pageSize) })
    if (query.source) params.set('source', query.source)
    if (query.search) params.set('search', query.search)
    return apiRequest<JsonResponse<operations['browseLocalRepositoryContent'], 200>>(
      `${localRepositoryPath(siteId)}/content?${params}`,
    )
  }

  async replaceSpec(siteId: string, spec: LocalRepositorySpecWrite): Promise<LocalRepository> {
    // The write replaces the whole spec and `serveDeployments` is required, so it is always sent.
    // The API refuses suites and components for every format but `deb`, and architectures for every
    // format but `deb` and `rpm`; the read shape has empty arrays there, so empty lists are left out.
    const body: JsonRequestBody<operations['putLocalRepositorySpec']> = {
      generation: spec.generation,
      serveDeployments: spec.serveDeployments,
      sources: spec.sources.map((source) => ({
        name: source.name,
        format: source.format,
        url: source.url,
        ...(source.suites.length > 0 ? { suites: [...source.suites] } : {}),
        ...(source.components.length > 0 ? { components: [...source.components] } : {}),
        ...(source.architectures.length > 0 ? { architectures: [...source.architectures] } : {}),
      })),
    }
    try {
      return await apiRequest<JsonResponse<operations['putLocalRepositorySpec'], 200>>(`${localRepositoryPath(siteId)}/spec`, {
        method: 'PUT',
        body: JSON.stringify(body),
      })
    } catch (error) {
      if (error instanceof ApiRequestError && error.status === 409) {
        throw new LocalRepositorySpecConflictError(error.message, error.requestId)
      }
      throw error
    }
  }

  async requestSync(siteId: string): Promise<void> {
    // The 202 body is the plain-text status text `Accepted`, which the contract says to ignore.
    await apiRequestText(`${localRepositoryPath(siteId)}/sync`, { method: 'POST' })
  }
}

function localRepositoryPath(siteId: string): string {
  return `/api/v1/sites/${encodeURIComponent(siteId)}/local-repository`
}
