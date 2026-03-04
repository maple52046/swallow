import type { SSHKey } from '@/domain/asset/types'
import type { AccessRepository } from '@/application/ports/AccessRepository'

export class ListSSHKeysUseCase {
  constructor(private readonly repo: AccessRepository) {}

  async execute(): Promise<SSHKey[]> {
    return this.repo.listSSHKeys()
  }
}
