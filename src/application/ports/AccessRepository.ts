import type { Connection, SSHKey, AccessPolicy } from '@/domain/asset/types'

export interface UpsertConnectionInput {
  id?: string
  name: string
  type: string
  host: string
  port: number
  username: string
  labels: string[]
}

export interface AccessRepository {
  listConnections(): Promise<Connection[]>
  upsertConnection(input: UpsertConnectionInput): Promise<Connection>
  deleteConnection(id: string): Promise<void>
  listSSHKeys(): Promise<SSHKey[]>
  listPolicies(): Promise<AccessPolicy[]>
}
