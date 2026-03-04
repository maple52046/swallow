export interface Agent {
  id: string
  name: string
  description: string
  version: string
  enabled: boolean
  status: 'running' | 'stopped' | 'error'
  capabilities: string[]
  lastHeartbeatAt?: string
}

export interface Model {
  id: string
  name: string
  provider: string
  description: string
  contextWindow: number
  isDefault: boolean
  enabled: boolean
  capabilities: string[]
  costPer1kTokens?: number
}

export interface PluginAction {
  name: string
  description: string
  requiredScopes: string[]
  parameters: Array<{ name: string; type: string; required: boolean; description: string }>
}

export interface Plugin {
  id: string
  name: string
  description: string
  version: string
  enabled: boolean
  category: 'infrastructure' | 'monitoring' | 'orchestration' | 'utility'
  actions: PluginAction[]
  scopes: string[]
  vendor?: string
}

export interface ProvisioningImage {
  id: string
  name: string
  os: string
  version: string
  arch: string
  size: string
  tags: string[]
  createdAt: string
}

export interface ProvisioningProfile {
  id: string
  name: string
  description: string
  imageId: string
  imageName: string
  packages: string[]
  scripts: string[]
  targetVendors: string[]
  createdAt: string
  updatedAt: string
}

export type ProvisioningJobStatus = 'pending' | 'running' | 'succeeded' | 'failed'

export interface ProvisioningJob {
  id: string
  profileId: string
  profileName: string
  targetHostId: string
  targetHostName: string
  status: ProvisioningJobStatus
  progress: number
  startedAt?: string
  completedAt?: string
  createdAt: string
  logs: string[]
}
