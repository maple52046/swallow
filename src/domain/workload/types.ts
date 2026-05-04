export type WorkloadStatus = 'running' | 'stopped' | 'exited' | 'created' | 'restarting'

export interface Container {
  id: string
  name: string
  image: string
  state: WorkloadStatus
  restarts: number
  startedAt: string
}
