import type { Run, RunStep } from '@/domain/run/types'

type UpdateRunFn = (id: string, partial: Partial<Run>) => void
type UpdateStepFn = (runId: string, stepIndex: number, partial: Partial<RunStep>) => void
type AppendLogFn = (runId: string, line: string) => void

export class RunSimulationEngine {
  private timers: Map<string, ReturnType<typeof setTimeout>> = new Map()

  constructor(
    private readonly updateRun: UpdateRunFn,
    private readonly updateStep: UpdateStepFn,
    private readonly appendLog: AppendLogFn,
  ) {}

  start(run: Run): void {
    const ts = () => new Date().toLocaleTimeString()
    this.appendLog(run.id, `[${ts()}] Run queued (trigger: ${run.trigger})`)
    const queuedTimer = setTimeout(() => {
      this.updateRun(run.id, { status: 'running', startedAt: new Date().toISOString() })
      this.appendLog(run.id, `[${ts()}] Run started — modelId: ${run.modelId}, target: ${run.target}`)
      this.appendLog(run.id, `[${ts()}] Goal: ${run.goal.slice(0, 120)}`)
      this.advanceStep(run.id, run.steps, 0)
    }, 1200)
    this.timers.set(`${run.id}:queued`, queuedTimer)
  }

  private advanceStep(runId: string, steps: RunStep[], idx: number): void {
    if (idx >= steps.length) { this.completeRun(runId); return }
    const ts = () => new Date().toLocaleTimeString()
    const step = steps[idx]
    this.updateStep(runId, idx, { status: 'running', startedAt: new Date().toISOString() })
    this.appendLog(runId, `[${ts()}] Step ${idx + 1}/${steps.length}: ${step.name} — starting...`)
    const delay = 1800 + Math.random() * 2000
    const t = setTimeout(() => {
      const now = new Date().toISOString()
      this.updateStep(runId, idx, { status: 'succeeded', completedAt: now, durationMs: Math.round(delay) })
      this.appendLog(runId, `[${ts()}] Step ${idx + 1}: ${step.name} — completed`)
      this.advanceStep(runId, steps, idx + 1)
    }, delay)
    this.timers.set(`${runId}:step${idx}`, t)
  }

  private completeRun(runId: string): void {
    const ts = () => new Date().toLocaleTimeString()
    const now = new Date().toISOString()
    this.appendLog(runId, `[${ts()}] All steps completed. Generating artifacts...`)
    const t = setTimeout(() => {
      this.appendLog(runId, `[${ts()}] Run completed successfully.`)
      this.updateRun(runId, {
        status: 'succeeded', completedAt: now,
        artifacts: [
          { id: `art-${runId}-1`, name: 'run-output.json', type: 'json', url: `/artifacts/${runId}/output.json`, sizeBytes: 24000 + Math.floor(Math.random() * 50000), createdAt: now },
          { id: `art-${runId}-2`, name: 'Run Report', type: 'report', url: `/artifacts/${runId}/report.html`, sizeBytes: 80000 + Math.floor(Math.random() * 100000), createdAt: now },
        ],
      })
    }, 800)
    this.timers.set(`${runId}:complete`, t)
  }

  cancel(runId: string): void {
    for (const [key, timer] of this.timers.entries()) {
      if (key.startsWith(`${runId}:`)) { clearTimeout(timer); this.timers.delete(key) }
    }
  }
}
