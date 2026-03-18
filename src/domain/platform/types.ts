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

export interface BaseModel {
  id: string
  name: string
  description: string
  isDefault: boolean
  enabled: boolean
  capabilities: string[]
}

export interface PublicModel extends BaseModel {
  type: 'public'
  provider: string
  accessKey?: string
  contextWindow: number
  costPer1kTokens?: number
}

export type ServerFramework = 'ollama' | 'sglang' | 'vllm'

export interface LocalModel extends BaseModel {
  type: 'local'
  serverHost: string
  serverPort: number
  framework: ServerFramework
}

export type Model = PublicModel | LocalModel
export type CreateModelInput = Omit<PublicModel, 'id'> | Omit<LocalModel, 'id'>

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
