import type { Connection, SSHKey, AccessPolicy } from '@/domain/asset/types'
import type { AccessRepository, UpsertConnectionInput } from '@/application/ports/AccessRepository'
import { seedConnections, seedSSHKeys } from '@/infrastructure/mock/data/seedHosts'

function genId() {
  return 'conn-' + Date.now().toString(36) + Math.random().toString(36).slice(2, 5)
}

export class MockAccessRepository implements AccessRepository {
  private connections: Map<string, Connection>
  private sshKeys: SSHKey[]
  private policies: AccessPolicy[]

  constructor() {
    this.connections = new Map(seedConnections.map((c) => [c.id, c]))
    this.sshKeys = [...seedSSHKeys]
    this.policies = [
      { id: 'policy-ops', name: 'Ops Full Access', rules: ['allow: ssh:*', 'allow: ipmi:*', 'deny: bmc:power-cycle'], createdAt: new Date(Date.now() - 90 * 86400000).toISOString() },
      { id: 'policy-readonly', name: 'Read Only', rules: ['allow: ssh:connect', 'allow: gpu:read', 'deny: *:write'], createdAt: new Date(Date.now() - 60 * 86400000).toISOString() },
    ]
  }

  async listConnections(): Promise<Connection[]> {
    return Array.from(this.connections.values()).sort((a, b) => a.name.localeCompare(b.name))
  }

  async upsertConnection(input: UpsertConnectionInput): Promise<Connection> {
    const now = new Date().toISOString()
    if (input.id && this.connections.has(input.id)) {
      const existing = this.connections.get(input.id)!
      const updated: Connection = { ...existing, ...input, type: input.type as Connection['type'], updatedAt: now }
      this.connections.set(input.id, updated)
      return updated
    }
    const conn: Connection = {
      id: genId(),
      name: input.name,
      type: input.type as Connection['type'],
      host: input.host,
      port: input.port,
      username: input.username,
      labels: input.labels,
      createdAt: now,
      updatedAt: now,
    }
    this.connections.set(conn.id, conn)
    return conn
  }

  async deleteConnection(id: string): Promise<void> {
    this.connections.delete(id)
  }

  async listSSHKeys(): Promise<SSHKey[]> {
    return [...this.sshKeys]
  }

  async listPolicies(): Promise<AccessPolicy[]> {
    return [...this.policies]
  }
}
