import type { SSHKey, ImportSSHKeyInput } from '@/domain/asset/types'
import type { AccessRepository } from '@/application/ports/AccessRepository'

export class ImportSSHKeyUseCase {
  constructor(private readonly repo: AccessRepository) {}

  async execute(input: ImportSSHKeyInput): Promise<SSHKey> {
    return this.repo.importSSHKey(input)
  }
}
