import type { Paginated, ServerRepository } from '@/application/ports/ServerRepository'
import type { NetworkLinkInput, NetworkTarget, ProvisioningTask } from '@/domain/provisioning/types'
import type {
  BootMediaProbe,
  DeployServerInput,
  HypervisorVirtualMachines,
  ListServersFilters,
  PowerStateResult,
  ProvisionerDetail,
  ReleaseServerInput,
  ProviderEvents,
  ProvisioningActionResult,
  Server,
  ServerAction,
  ServerBootMedia,
  ServerPowerConfiguration,
  SetServerBootMediaResult,
  SetServerDefaultUserInput,
  SetServerDefaultUserResult,
  SetServerPowerConfigurationInput,
} from '@/domain/server/types'
import { ApiRequestError, apiRequest } from './client'

/** The Power Configuration as the API sends it; the enum fields are narrowed by mapPowerConfiguration. */
interface PowerConfigurationDTO extends Omit<ServerPowerConfiguration, 'family' | 'control' | 'drivers'> {
  family: string
  control: string
  drivers: { driver: string; family: string }[] | null
}

/**
 * Narrows the API's Power Configuration onto the domain type. Unknown enum values a newer API might
 * send are read safely: an unknown family becomes null (no BMC functions, so no Boot Media offered)
 * and an unknown control becomes `unknown`; driver options of an unknown family are dropped because
 * the form would not know their parameters.
 */
function mapPowerConfiguration(dto: PowerConfigurationDTO): ServerPowerConfiguration {
  const family = (value: string): ServerPowerConfiguration['family'] =>
    value === 'bmc' || value === 'virsh' ? value : null
  const control: ServerPowerConfiguration['control'] =
    dto.control === 'none' || dto.control === 'manual' || dto.control === 'automatic' ? dto.control : 'unknown'
  return {
    ...dto,
    family: family(dto.family),
    control,
    drivers: (dto.drivers ?? []).flatMap((option) => {
      const optionFamily = family(option.family)
      return optionFamily ? [{ driver: option.driver, family: optionFamily }] : []
    }),
  }
}

/**
 * The API response shape maps directly onto the domain type, so there is no adapter
 * layer here. That is deliberate: the contract was designed so that a client does not
 * have to reshape anything, and a translation step would only be somewhere for the two
 * to drift apart.
 */
export class ApiServerRepository implements ServerRepository {
  async listServers(filters?: ListServersFilters): Promise<Paginated<Server>> {
    const query = new URLSearchParams()
    if (filters?.siteId) query.set('siteId', filters.siteId)
    if (filters?.integrationId) query.set('integrationId', filters.integrationId)
    if (filters?.provisioningState) query.set('provisioningState', filters.provisioningState)
    if (filters?.platformId) query.set('platformId', filters.platformId)
    if (filters?.keyword) query.set('keyword', filters.keyword)
    if (filters?.includeAbsent) query.set('includeAbsent', 'true')
    if (filters?.page) query.set('page', String(filters.page))
    if (filters?.pageSize) query.set('pageSize', String(filters.pageSize))

    const suffix = query.toString() ? `?${query.toString()}` : ''
    return apiRequest<Paginated<Server>>(`/api/v1/servers${suffix}`, { cache: 'no-store' })
  }

  async getServer(id: string): Promise<Server | null> {
    try {
      return await apiRequest<Server>(`/api/v1/servers/${encodeURIComponent(id)}`)
    } catch (error) {
      // A missing server is an expected answer for a stale link, not a failure to
      // report to the user as an error.
      if (error instanceof ApiRequestError && error.status === 404) {
        return null
      }
      throw error
    }
  }

  async deployServer(id: string, input: DeployServerInput): Promise<ProvisioningActionResult> {
    return apiRequest<ProvisioningActionResult>(
      `/api/v1/servers/${encodeURIComponent(id)}/deploy`,
      { method: 'POST', body: JSON.stringify(input) },
    )
  }

  async releaseServer(id: string, input?: ReleaseServerInput): Promise<ProvisioningActionResult> {
    return apiRequest<ProvisioningActionResult>(
      `/api/v1/servers/${encodeURIComponent(id)}/release`,
      input
        ? { method: 'POST', body: JSON.stringify(input) }
        : { method: 'POST' },
    )
  }

  async refreshServer(id: string): Promise<ProvisioningActionResult> {
    return apiRequest<ProvisioningActionResult>(
      '/api/v1/servers/' + encodeURIComponent(id) + '/refresh',
      { method: 'POST' },
    )
  }

  async deleteServer(id: string): Promise<void> {
    await apiRequest<void>(
      `/api/v1/servers/${encodeURIComponent(id)}`,
      { method: 'DELETE' },
    )
  }

  async getProvisionerDetail(id: string): Promise<ProvisionerDetail> {
    return apiRequest<ProvisionerDetail>(
      `/api/v1/servers/${encodeURIComponent(id)}/provisioner-detail`,
    )
  }

  async getProviderEvents(id: string, limit = 50): Promise<ProviderEvents> {
    const path = `/api/v1/servers/${encodeURIComponent(id)}/events?limit=${encodeURIComponent(String(limit))}`
    return apiRequest<ProviderEvents>(path)
  }

  async getNetwork(id: string): Promise<NetworkTarget> {
    return apiRequest<NetworkTarget>('/api/v1/servers/' + encodeURIComponent(id) + '/network')
  }

  async createNetworkLink(id: string, interfaceId: string, input: NetworkLinkInput): Promise<NetworkTarget> {
    const path = '/api/v1/servers/' + encodeURIComponent(id) + '/network/interfaces/' + encodeURIComponent(interfaceId) + '/links'
    return apiRequest<NetworkTarget>(path, { method: 'POST', body: JSON.stringify(input) })
  }

  async replaceNetworkLink(id: string, interfaceId: string, linkId: string, input: NetworkLinkInput): Promise<NetworkTarget> {
    const path = '/api/v1/servers/' + encodeURIComponent(id) + '/network/interfaces/' + encodeURIComponent(interfaceId) + '/links/' + encodeURIComponent(linkId)
    return apiRequest<NetworkTarget>(path, { method: 'PUT', body: JSON.stringify(input) })
  }

  async deleteNetworkLink(id: string, interfaceId: string, linkId: string): Promise<NetworkTarget> {
    const path = '/api/v1/servers/' + encodeURIComponent(id) + '/network/interfaces/' + encodeURIComponent(interfaceId) + '/links/' + encodeURIComponent(linkId)
    return apiRequest<NetworkTarget>(path, { method: 'DELETE' })
  }

  async listProvisioningTasks(id: string): Promise<ProvisioningTask[]> {
    return apiRequest<ProvisioningTask[]>('/api/v1/servers/' + encodeURIComponent(id) + '/provisioning-tasks')
  }

  async getProvisioningTask(taskId: string): Promise<ProvisioningTask> {
    return apiRequest<ProvisioningTask>('/api/v1/provisioning/tasks/' + encodeURIComponent(taskId))
  }

  async retryProvisioningTask(taskId: string): Promise<ProvisioningTask> {
    return apiRequest<ProvisioningTask>('/api/v1/provisioning/tasks/' + encodeURIComponent(taskId) + '/retry', { method: 'POST' })
  }

  async runServerAction(id: string, action: ServerAction): Promise<ProvisioningActionResult> {
    return apiRequest<ProvisioningActionResult>(
      `/api/v1/servers/${encodeURIComponent(id)}/${action}`,
      { method: 'POST' },
    )
  }

  async queryPowerState(id: string): Promise<PowerStateResult> {
    return apiRequest<PowerStateResult>(
      `/api/v1/servers/${encodeURIComponent(id)}/power-state`,
    )
  }

  async getPowerConfiguration(id: string): Promise<ServerPowerConfiguration> {
    // Never cached: it is the provisioner's live state an operator is about to edit.
    const dto = await apiRequest<PowerConfigurationDTO>(
      `/api/v1/servers/${encodeURIComponent(id)}/power-configuration`,
      { cache: 'no-store' },
    )
    return mapPowerConfiguration(dto)
  }

  /**
   * Sends only the parameters the input carries. `password` is included only when it is defined
   * (`''` clears it), so an omitted password keeps the provisioner's; it is not retained here, and
   * the shared client never logs request bodies.
   */
  async setPowerConfiguration(id: string, input: SetServerPowerConfigurationInput): Promise<ServerPowerConfiguration> {
    const body: SetServerPowerConfigurationInput = { driver: input.driver, address: input.address }
    if (input.powerId !== undefined) body.powerId = input.powerId
    if (input.username !== undefined) body.username = input.username
    if (input.password !== undefined) body.password = input.password
    const dto = await apiRequest<PowerConfigurationDTO>(
      `/api/v1/servers/${encodeURIComponent(id)}/power-configuration`,
      { method: 'PUT', body: JSON.stringify(body) },
    )
    return mapPowerConfiguration(dto)
  }

  /**
   * The password is sent only when present and only in this request body; it is not retained
   * here, and the shared client never logs request bodies.
   */
  async setDefaultUser(id: string, input: SetServerDefaultUserInput): Promise<SetServerDefaultUserResult> {
    const body: SetServerDefaultUserInput = input.password ? { user: input.user, password: input.password } : { user: input.user }
    return apiRequest<SetServerDefaultUserResult>(
      `/api/v1/servers/${encodeURIComponent(id)}/default-user`,
      { method: 'PUT', body: JSON.stringify(body) },
    )
  }

  async clearDefaultUser(id: string): Promise<void> {
    await apiRequest<void>(
      `/api/v1/servers/${encodeURIComponent(id)}/default-user`,
      { method: 'DELETE' },
    )
  }

  async getBootMedia(id: string, options?: { live?: boolean }): Promise<ServerBootMedia> {
    const suffix = options?.live ? '?live=true' : ''
    // Never cached: while a preflight runs this read is polled for its progress (`apply`).
    const media = await apiRequest<ServerBootMedia>(
      `/api/v1/servers/${encodeURIComponent(id)}/boot-media${suffix}`,
      { cache: 'no-store' },
    )
    return normalizeBootMedia(media)
  }

  async setBootMedia(id: string, enabled: boolean, isoId?: string): Promise<SetServerBootMediaResult> {
    // The API ignores isoId on a disable; it is sent only to enable.
    const body = enabled ? { enabled, isoId } : { enabled }
    const result = await apiRequest<SetServerBootMediaResult>(
      `/api/v1/servers/${encodeURIComponent(id)}/boot-media`,
      { method: 'PUT', body: JSON.stringify(body) },
    )
    return normalizeBootMedia(result)
  }

  async probeBootMedia(id: string): Promise<BootMediaProbe> {
    // The route kept its Redfish-era path when it learned libvirt (decision 055).
    const result = await apiRequest<Partial<BootMediaProbe>>(
      `/api/v1/servers/${encodeURIComponent(id)}/redfish/probe`,
      { method: 'POST' },
    )
    return { redfish: result.redfish ?? null, libvirt: result.libvirt ?? null }
  }

  async listVirtualMachines(hypervisorId: string, account?: string): Promise<HypervisorVirtualMachines> {
    const query = account?.trim() ? `?account=${encodeURIComponent(account.trim())}` : ''
    // Never cached: it is a live libvirt read, and a domain's state changes as it is started or enrolled.
    const result = await apiRequest<HypervisorVirtualMachines>(
      `/api/v1/servers/${encodeURIComponent(hypervisorId)}/virtual-machines${query}`,
      { cache: 'no-store' },
    )
    return { ...result, items: (result.items ?? []).map((item) => ({ ...item, macAddresses: item.macAddresses ?? [], serverId: item.serverId ?? null })) }
  }
}

/**
 * Fills the Boot Media fields an older API omits — `apply`, and `method` and `libvirt` from before
 * libvirt Boot Media (decision 055) — with their documented "none" value, so the panel never reads
 * `undefined` as a method.
 */
function normalizeBootMedia<T extends ServerBootMedia>(media: T): T {
  return { ...media, method: media.method ?? null, libvirt: media.libvirt ?? null, apply: media.apply ?? null }
}
