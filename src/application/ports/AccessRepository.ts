import type { Connection, SSHKey, AccessPolicy, ImportSSHKeyInput } from '@/domain/asset/types'

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
  importSSHKey(input: ImportSSHKeyInput): Promise<SSHKey>
  deleteSSHKey(id: string): Promise<void>
  listPolicies(): Promise<AccessPolicy[]>
}
