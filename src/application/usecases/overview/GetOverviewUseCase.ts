import type { OverviewData } from '@/application/dtos'
import type { MissionRepository } from '@/application/ports/MissionRepository'
import type { RunRepository } from '@/application/ports/RunRepository'
import type { ObservabilityRepository } from '@/application/ports/ObservabilityRepository'
import type { PlaneRepository } from '@/application/ports/PlaneRepository'

export class GetOverviewUseCase {
  constructor(
    private readonly missionRepo: MissionRepository,
    private readonly runRepo: RunRepository,
    private readonly obsRepo: ObservabilityRepository,
    private readonly planeRepo: PlaneRepository,
  ) {}

  async execute(): Promise<OverviewData> {
    const [missions, recentRuns, activeAlerts, topCriticalGPUs, planes] = await Promise.all([
      this.missionRepo.list(),
      this.runRepo.list({ limit: 10 }),
      this.obsRepo.listAlerts({ status: 'active', limit: 10 }),
      this.obsRepo.getTopCriticalGPUs(5),
      this.planeRepo.list(),
    ])

    const allRuns = await this.runRepo.list()

    const opsTimeline = [
      ...recentRuns.slice(0, 5).map((r) => ({
        id: `run-${r.id}`,
        type: (r.status === 'succeeded'
          ? 'run_completed'
          : r.status === 'failed'
            ? 'run_failed'
            : 'run_started') as OverviewData['opsTimeline'][number]['type'],
        title: `Run ${r.status}: ${r.missionName}`,
        description: `Triggered by ${r.trigger} on ${r.target}`,
        timestamp: r.completedAt ?? r.startedAt ?? r.queuedAt,
        severity: (r.status === 'failed' ? 'critical' : 'info') as 'info' | 'critical',
        resourceId: r.id,
        resourceType: 'run',
      })),
      ...activeAlerts.slice(0, 5).map((a) => ({
        id: `alert-${a.id}`,
        type: 'alert_fired' as const,
        title: `Alert: ${a.title}`,
        description: a.message,
        timestamp: a.createdAt,
        severity: a.severity as 'info' | 'warning' | 'critical',
        resourceId: a.id,
        resourceType: 'alert',
      })),
    ].sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime())

    return {
      kpis: {
        activeMissions: missions.filter((m) => m.status === 'active').length,
        totalMissions: missions.length,
        runningRuns: allRuns.filter((r) => r.status === 'running' || r.status === 'queued').length,
        totalRuns: allRuns.length,
        activeAlerts: activeAlerts.length,
        criticalGPUs: topCriticalGPUs.filter((g) => g.health === 'critical').length,
        connectedPlanes: planes.filter((p) => p.status === 'connected').length,
      },
      recentRuns,
      topCriticalGPUs,
      activeAlerts,
      opsTimeline,
    }
  }
}
