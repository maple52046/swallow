import type { Plane } from '@/domain/plane/types'
import type { PlaneRepository } from '@/application/ports/PlaneRepository'

export class ListPlanesUseCase {
  constructor(private readonly repo: PlaneRepository) {}

  async execute(): Promise<Plane[]> {
    return this.repo.list()
  }
}
