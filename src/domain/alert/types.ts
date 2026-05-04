export type AlertSeverity = 'critical' | 'warning' | 'info'
export type AlertStatus = 'active' | 'acknowledged' | 'resolved'
export type AlertCategory = 'gpu' | 'server' | 'network' | 'storage' | 'plane' | 'system'

export interface Alert {
  id: string
  title: string
  message: string
  severity: AlertSeverity
  status: AlertStatus
  category: AlertCategory
  relatedAssetIds: string[]
  relatedAssetNames: string[]
  suggestedMissionGoal?: string
  suggestedPlugins?: string[]
  suggestedTarget?: string
  createdAt: string
  acknowledgedAt?: string
  resolvedAt?: string
  acknowledgedBy?: string
}
