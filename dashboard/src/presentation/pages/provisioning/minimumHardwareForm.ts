import type { MinimumHardware } from '@/domain/provisioning/types'

/**
 * The template editor's minimum hardware inputs as typed text (provisioning.md "Deployment
 * Templates"). An empty field sets no floor for its dimension. Memory is typed in GiB, the unit a
 * person thinks in, and sent as the contract's MiB.
 */
export interface MinimumHardwareDraft {
  cpuCores: string
  memoryGiB: string
  storageGB: string
  gpuModel: string
  gpuCount: string
}

/** A draft with no floor in any dimension. */
export const EMPTY_MINIMUM_HARDWARE_DRAFT: MinimumHardwareDraft = { cpuCores: '', memoryGiB: '', storageGB: '', gpuModel: '', gpuCount: '' }

/** Fills the editor from a stored floor; a template without one reads as the empty draft. */
export function minimumHardwareDraft(minimum: MinimumHardware | null): MinimumHardwareDraft {
  if (!minimum) return EMPTY_MINIMUM_HARDWARE_DRAFT
  return {
    cpuCores: minimum.cpuCores === null ? '' : String(minimum.cpuCores),
    memoryGiB: minimum.memoryMiB === null ? '' : String(Math.round((minimum.memoryMiB / 1024) * 100) / 100),
    storageGB: minimum.storageGB === null ? '' : String(minimum.storageGB),
    gpuModel: minimum.gpuModel ?? '',
    gpuCount: minimum.gpuCount === null ? '' : String(minimum.gpuCount),
  }
}

/**
 * Reads the draft as the floor the API takes, or names the first field that is not a positive
 * number. Every dimension left empty yields a floor of nulls, which clears a stored floor on update.
 */
export function parseMinimumHardware(draft: MinimumHardwareDraft): { minimum: MinimumHardware } | { error: string } {
  const whole = (text: string, label: string): number | null | string => {
    const value = text.trim()
    if (value === '') return null
    const number = Number(value)
    return Number.isInteger(number) && number > 0 ? number : `${label} must be a whole number greater than 0.`
  }
  const cpuCores = whole(draft.cpuCores, 'CPU cores')
  const storageGB = whole(draft.storageGB, 'Storage')
  const gpuCount = whole(draft.gpuCount, 'GPU count')
  let memoryMiB: number | null | string = null
  if (draft.memoryGiB.trim() !== '') {
    const gib = Number(draft.memoryGiB.trim())
    memoryMiB = Number.isFinite(gib) && gib > 0 ? Math.round(gib * 1024) : 'Memory must be a number of GiB greater than 0.'
  }
  for (const value of [cpuCores, memoryMiB, storageGB, gpuCount]) {
    if (typeof value === 'string') return { error: value }
  }
  return {
    minimum: {
      cpuCores: cpuCores as number | null,
      memoryMiB: memoryMiB as number | null,
      storageGB: storageGB as number | null,
      gpuModel: draft.gpuModel.trim() || null,
      gpuCount: gpuCount as number | null,
    },
  }
}

/** Whether a floor constrains nothing, so a create can leave it out. */
export function minimumHardwareIsEmpty(minimum: MinimumHardware): boolean {
  return Object.values(minimum).every((value) => value === null)
}

/** A floor in words for recipe cards, for example "32 cores · 256 GiB · 8× MI300X"; "None" without one. */
export function formatMinimumHardware(minimum: MinimumHardware | null): string {
  if (!minimum || minimumHardwareIsEmpty(minimum)) return 'None'
  const parts: string[] = []
  if (minimum.cpuCores !== null) parts.push(`${minimum.cpuCores} cores`)
  if (minimum.memoryMiB !== null) parts.push(`${Math.round((minimum.memoryMiB / 1024) * 100) / 100} GiB memory`)
  if (minimum.storageGB !== null) parts.push(`${minimum.storageGB} GB storage`)
  if (minimum.gpuCount !== null || minimum.gpuModel !== null) {
    parts.push(`${minimum.gpuCount ?? 'any number of'} ${minimum.gpuModel ?? ''} GPUs`.replace(/\s+/g, ' ').trim())
  }
  return parts.join(' · ')
}
