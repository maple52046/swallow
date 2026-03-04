import type { MissionTrigger, MissionPlan, MissionPermissions } from '@/domain/mission/types'

export interface CreateMissionDto {
  name: string
  goal: string
  model: string
  target: string
  trigger: MissionTrigger
  schedule?: string
  plan: MissionPlan
  permissions: MissionPermissions
  tags?: string[]
}

export interface UpdateMissionPlanDto {
  missionId: string
  plan: MissionPlan
}

export interface RunMissionNowDto {
  missionId: string
  trigger?: 'manual' | 'rerun'
}

export interface CreateMissionFromAlertDto {
  alertId: string
  goal: string
  target: string
  plugins: string[]
}

export interface OverviewData {
  kpis: {
    activeMissions: number
    totalMissions: number
    runningRuns: number
    totalRuns: number
    activeAlerts: number
    criticalGPUs: number
    connectedPlanes: number
  }
  recentRuns: import('@/domain/run/types').Run[]
  topCriticalGPUs: Array<import('@/domain/gpu/types').GPUDevice & { metrics: import('@/domain/gpu/types').GPUMetrics }>
  activeAlerts: import('@/domain/alert/types').Alert[]
  opsTimeline: OpsTimelineEvent[]
}

export interface OpsTimelineEvent {
  id: string
  type: 'run_started' | 'run_completed' | 'run_failed' | 'alert_fired' | 'alert_resolved' | 'plane_event'
  title: string
  description: string
  timestamp: string
  severity?: 'info' | 'warning' | 'critical'
  resourceId?: string
  resourceType?: string
}
