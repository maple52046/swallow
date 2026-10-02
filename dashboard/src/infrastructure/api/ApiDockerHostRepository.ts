import type { DockerHostRepository } from '@/application/ports/DockerHostRepository'
import type {
  CreateDockerContainerInput,
  CreateDockerNetworkInput,
  CreateDockerVolumeInput,
  DockerContainer,
  DockerContainerAction,
  DockerContainerCreated,
  DockerEngineSummary,
  DockerImage,
  DockerImagePullResult,
  DockerNetwork,
  DockerVolume,
} from '@/domain/software/docker'
import { apiRequest } from './client'

/** The `{ items: [...] }` envelope every explorer list returns (bounded per host, not paginated). */
interface ItemsEnvelope<T> {
  items: T[]
}

/**
 * Maps the Active Docker Host Explorer contract (`/api/v1/servers/{id}/docker`, `servers-docker.md`)
 * onto the DockerHostRepository port.
 *
 * The contract JSON already matches the domain shapes, so responses are returned directly. Engine
 * ids and names are URL-encoded into path segments because image ids contain `:`. Errors surface as
 * the shared `ApiRequestError` (with `status` and the API's message) from `apiRequest`. No credential
 * for the host crosses this boundary: api-server alone talks to the Engine.
 */
export class ApiDockerHostRepository implements DockerHostRepository {
  private base(serverId: string): string {
    return `/api/v1/servers/${encodeURIComponent(serverId)}/docker`
  }

  async summary(serverId: string): Promise<DockerEngineSummary> {
    return apiRequest<DockerEngineSummary>(this.base(serverId))
  }

  async listImages(serverId: string): Promise<DockerImage[]> {
    return (await apiRequest<ItemsEnvelope<DockerImage>>(`${this.base(serverId)}/images`)).items
  }

  async pullImage(serverId: string, reference: string): Promise<DockerImagePullResult> {
    return apiRequest<DockerImagePullResult>(`${this.base(serverId)}/images/pull`, {
      method: 'POST',
      body: JSON.stringify({ reference }),
    })
  }

  async removeImage(serverId: string, imageId: string, force: boolean): Promise<void> {
    await apiRequest(`${this.base(serverId)}/images/${encodeURIComponent(imageId)}?force=${force}`, { method: 'DELETE' })
  }

  async listContainers(serverId: string): Promise<DockerContainer[]> {
    return (await apiRequest<ItemsEnvelope<DockerContainer>>(`${this.base(serverId)}/containers`)).items
  }

  async createContainer(serverId: string, input: CreateDockerContainerInput): Promise<DockerContainerCreated> {
    return apiRequest<DockerContainerCreated>(`${this.base(serverId)}/containers`, {
      method: 'POST',
      body: JSON.stringify(input),
    })
  }

  async actOnContainer(serverId: string, containerId: string, action: DockerContainerAction): Promise<void> {
    await apiRequest(`${this.base(serverId)}/containers/${encodeURIComponent(containerId)}/${action}`, { method: 'POST' })
  }

  async removeContainer(serverId: string, containerId: string, options: { force: boolean; removeVolumes: boolean }): Promise<void> {
    const query = new URLSearchParams({ force: String(options.force), removeVolumes: String(options.removeVolumes) })
    await apiRequest(`${this.base(serverId)}/containers/${encodeURIComponent(containerId)}?${query}`, { method: 'DELETE' })
  }

  async containerLogs(serverId: string, containerId: string, tailLines: number): Promise<string> {
    const response = await apiRequest<{ logs: string }>(
      `${this.base(serverId)}/containers/${encodeURIComponent(containerId)}/logs?tailLines=${tailLines}`,
    )
    return response.logs
  }

  async listVolumes(serverId: string): Promise<DockerVolume[]> {
    return (await apiRequest<ItemsEnvelope<DockerVolume>>(`${this.base(serverId)}/volumes`)).items
  }

  async createVolume(serverId: string, input: CreateDockerVolumeInput): Promise<DockerVolume> {
    return apiRequest<DockerVolume>(`${this.base(serverId)}/volumes`, { method: 'POST', body: JSON.stringify(input) })
  }

  async removeVolume(serverId: string, name: string, force: boolean): Promise<void> {
    await apiRequest(`${this.base(serverId)}/volumes/${encodeURIComponent(name)}?force=${force}`, { method: 'DELETE' })
  }

  async listNetworks(serverId: string): Promise<DockerNetwork[]> {
    return (await apiRequest<ItemsEnvelope<DockerNetwork>>(`${this.base(serverId)}/networks`)).items
  }

  async createNetwork(serverId: string, input: CreateDockerNetworkInput): Promise<DockerNetwork> {
    return apiRequest<DockerNetwork>(`${this.base(serverId)}/networks`, { method: 'POST', body: JSON.stringify(input) })
  }

  async removeNetwork(serverId: string, networkId: string): Promise<void> {
    await apiRequest(`${this.base(serverId)}/networks/${encodeURIComponent(networkId)}`, { method: 'DELETE' })
  }
}
