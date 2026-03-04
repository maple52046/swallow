import type { Plane } from '@/domain/plane/types'
import type { PlaneRepository, RegisterPlaneInput } from '@/application/ports/PlaneRepository'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class RegisterPlaneUseCase {
  constructor(
    private readonly planeRepo: PlaneRepository,
    private readonly platformRepo: PlatformRepository,
  ) {}

  async execute(input: RegisterPlaneInput): Promise<Plane> {
    const plane = await this.planeRepo.register(input)
    await this.platformRepo.appendAuditEvent({
      type: 'plane.registered',
      actor: 'user',
      resourceType: 'plane',
      resourceId: plane.id,
      resourceName: plane.name,
      description: `Management plane "${plane.name}" registered (${plane.type})`,
      timestamp: new Date().toISOString(),
    })
    return plane
  }
}
