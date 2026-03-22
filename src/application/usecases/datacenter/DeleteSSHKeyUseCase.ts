import type { AccessRepository } from '@/application/ports/AccessRepository'

export class DeleteSSHKeyUseCase {
  constructor(private readonly repo: AccessRepository) {}

  async execute(id: string): Promise<void> {
    return this.repo.deleteSSHKey(id)
  }
}
