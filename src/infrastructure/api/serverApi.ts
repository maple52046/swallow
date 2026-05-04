import { apiRequest } from './client'
import type {
  CreateServerRequest,
  CreateServerResponse,
  DeleteServerResponse,
  ListServersParams,
  ServerListResponse,
} from './types'

export async function listServers(params: ListServersParams = {}): Promise<ServerListResponse> {
  const query = new URLSearchParams()
  if (params.page !== undefined) query.set('page', String(params.page))
  if (params.pageSize !== undefined) query.set('pageSize', String(params.pageSize))
  if (params.status) query.set('status', params.status)
  if (params.keyword) query.set('keyword', params.keyword)

  const qs = query.toString()
  return apiRequest<ServerListResponse>(`/api/v1/servers/${qs ? `?${qs}` : ''}`)
}

export async function createServer(input: CreateServerRequest): Promise<CreateServerResponse> {
  return apiRequest<CreateServerResponse>('/api/v1/servers/', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export async function deleteServer(id: string): Promise<DeleteServerResponse> {
  return apiRequest<DeleteServerResponse>(`/api/v1/servers/${id}`, {
    method: 'DELETE',
  })
}
