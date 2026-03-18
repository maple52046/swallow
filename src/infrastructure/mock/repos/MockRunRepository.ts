import type { Run, RunStep } from '@/domain/run/types'
import type { RunRepository, CreateRunInput, ListRunsFilters } from '@/application/ports/RunRepository'
import { seedRuns } from '@/infrastructure/mock/data/seedRuns'
import { seedModels } from '@/infrastructure/mock/data/seedPlatform'
import { RunSimulationEngine } from '@/infrastructure/mock/simulation/RunSimulationEngine'
import { lsGet, lsSet } from '@/infrastructure/persistence/localStorage'

function genId() {
  return 'run-' + Date.now().toString(36) + Math.random().toString(36).slice(2, 6)
}

function resolveModelId(modelId: string | undefined, legacyModelName: string | undefined): string {
  if (modelId) return modelId
  const byName = legacyModelName ? seedModels.find((m) => m.name === legacyModelName) : undefined
  return byName?.id ?? seedModels.find((m) => m.isDefault)?.id ?? 'model-gpt4o'
}

export class MockRunRepository implements RunRepository {
  private store: Map<string, Run>
  private engine: RunSimulationEngine

  constructor() {
    const saved = lsGet<Array<Run & { model?: string }>>('runs', seedRuns as Array<Run & { model?: string }>)
    const normalized = saved.map((r) => ({
      ...r,
      modelId: resolveModelId(r.modelId, r.model),
    }))
    this.store = new Map(normalized.map((r) => [r.id, r]))
    this.engine = new RunSimulationEngine(
      (id, partial) => this._updateRun(id, partial),
      (id, stepIdx, partial) => this._updateStep(id, stepIdx, partial),
      (id, line) => this._appendLog(id, line),
    )
  }

  private save() {
    lsSet('runs', Array.from(this.store.values()))
  }

  _updateRun(id: string, partial: Partial<Run>): void {
    const existing = this.store.get(id)
    if (!existing) return
    this.store.set(id, { ...existing, ...partial })
    this.save()
  }

  _updateStep(runId: string, stepIndex: number, partial: Partial<RunStep>): void {
    const run = this.store.get(runId)
    if (!run) return
    const steps = [...run.steps]
    if (steps[stepIndex]) steps[stepIndex] = { ...steps[stepIndex], ...partial }
    this.store.set(runId, { ...run, steps })
    this.save()
  }

  _appendLog(runId: string, line: string): void {
    const run = this.store.get(runId)
    if (!run) return
    this.store.set(runId, { ...run, logs: [...run.logs, line] })
    this.save()
  }

  async list(filters?: ListRunsFilters): Promise<Run[]> {
    let items = Array.from(this.store.values())
    if (filters?.status) items = items.filter((r) => r.status === filters.status)
    if (filters?.missionId) items = items.filter((r) => r.missionId === filters.missionId)
    if (filters?.trigger) items = items.filter((r) => r.trigger === filters.trigger)
    if (filters?.search) {
      const q = filters.search.toLowerCase()
      items = items.filter((r) => r.missionName.toLowerCase().includes(q) || r.target.toLowerCase().includes(q))
    }
    items = items.sort((a, b) => new Date(b.queuedAt).getTime() - new Date(a.queuedAt).getTime())
    if (filters?.limit) items = items.slice(filters.offset ?? 0, (filters.offset ?? 0) + filters.limit)
    return items
  }

  async get(id: string): Promise<Run | null> {
    return this.store.get(id) ?? null
  }

  async create(input: CreateRunInput): Promise<Run> {
    const now = new Date().toISOString()
    const run: Run = {
      id: genId(),
      missionId: input.missionId,
      missionName: input.missionName,
      status: 'queued',
      trigger: input.trigger as Run['trigger'],
      modelId: input.modelId,
      target: input.target,
      goal: input.goal,
      steps: input.stepNames.map((name, i) => ({
        id: `step-${i + 1}`,
        order: i + 1,
        name,
        status: 'pending' as const,
      })),
      logs: [],
      artifacts: [],
      queuedAt: now,
    }
    this.store.set(run.id, run)
    this.save()
    this.engine.start(run)
    return run
  }

  async update(id: string, partial: Partial<Run>): Promise<Run> {
    const existing = this.store.get(id)
    if (!existing) throw new Error('Run not found: ' + id)
    const updated = { ...existing, ...partial }
    this.store.set(id, updated)
    this.save()
    return updated
  }

  async cancel(id: string): Promise<Run> {
    const run = this.store.get(id)
    if (!run) throw new Error('Run not found: ' + id)
    this.engine.cancel(id)
    const now = new Date().toISOString()
    const steps = run.steps.map((s) =>
      s.status === 'running' || s.status === 'pending' ? { ...s, status: 'skipped' as const } : s,
    )
    const updated = { ...run, status: 'canceled' as const, completedAt: now, steps }
    this.store.set(id, updated)
    this.save()
    return updated
  }
}
