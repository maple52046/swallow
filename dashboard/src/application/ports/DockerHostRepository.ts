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

/**
 * Application port for the Docker Host Explorer (decision 043): live management of the Docker Engine
 * on one Server through `api-server`, never by connecting to the host.
 *
 * Implementations map the Active `servers-docker.md` contract. Every call is a live Engine request;
 * nothing is cached or persisted. Rejections carry the API's HTTP `status` so callers can tell an
 * ineligible Server (409: Docker CE not installed by swallow, API disabled, or not a deployed host;
 * also a locked Server on writes) from a fault, and the API's human message for display. Writes are
 * refused by the server while the Server is locked; callers should not rely on hiding controls.
 */
export interface DockerHostRepository {
  summary(serverId: string): Promise<DockerEngineSummary>

  listImages(serverId: string): Promise<DockerImage[]>
  /** Pulls synchronously (bounded server-side to 55 minutes); an untagged reference pulls `latest`. */
  pullImage(serverId: string, reference: string): Promise<DockerImagePullResult>
  /** `force` also removes multi-tagged images or ones referenced by stopped containers. */
  removeImage(serverId: string, imageId: string, force: boolean): Promise<void>

  listContainers(serverId: string): Promise<DockerContainer[]>
  /** Resolves with the created container; a start failure after create rejects (the container remains). */
  createContainer(serverId: string, input: CreateDockerContainerInput): Promise<DockerContainerCreated>
  actOnContainer(serverId: string, containerId: string, action: DockerContainerAction): Promise<void>
  /** `force` kills a running container first; `removeVolumes` also removes its anonymous volumes. */
  removeContainer(serverId: string, containerId: string, options: { force: boolean; removeVolumes: boolean }): Promise<void>
  /** A bounded snapshot (not a stream) of the last `tailLines` lines of stdout and stderr. */
  containerLogs(serverId: string, containerId: string, tailLines: number): Promise<string>

  listVolumes(serverId: string): Promise<DockerVolume[]>
  createVolume(serverId: string, input: CreateDockerVolumeInput): Promise<DockerVolume>
  removeVolume(serverId: string, name: string, force: boolean): Promise<void>

  listNetworks(serverId: string): Promise<DockerNetwork[]>
  createNetwork(serverId: string, input: CreateDockerNetworkInput): Promise<DockerNetwork>
  removeNetwork(serverId: string, networkId: string): Promise<void>
}
