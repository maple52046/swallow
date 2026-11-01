import { moduleSettingsSpec } from '@/domain/module/settings'
import type { ModuleSpecSchema } from '@/domain/module/types'
import {
  SOFTWARE_INTEGER_SPEC_FIELDS,
  SOFTWARE_LIST_SPEC_FIELDS,
  type InstallSoftwareInput,
  type SoftwareAssignment,
  type SoftwareCatalogEntry,
  type SoftwareRole,
} from '@/domain/software/types'

/** Operator intent shown on the final Software installation button. */
export type SoftwareInstallMode = 'install' | 'retry' | 'reconfigure'

/** String and boolean form state derived from one existing assignment. */
export interface SoftwareInstallDefaults {
  roles: SoftwareRole[]
  spec: Record<string, string>
  enableApi: boolean
}

/**
 * Converts persisted assignment configuration into editable form values.
 * Unknown keys stay out of the form because the catalog's `specFields` remains authoritative.
 * A list is shown one entry per line so a reconfigure resends the recorded list unchanged, and a
 * number or boolean as its JSON text; `enableApi` has its own toggle.
 */
export function softwareInstallDefaults(
  entry: SoftwareCatalogEntry,
  assignment?: SoftwareAssignment,
): SoftwareInstallDefaults {
  const spec: Record<string, string> = {}
  for (const field of entry.specFields) {
    const value = assignment?.spec?.[field]
    if (field === 'enableApi') continue
    if (typeof value === 'string') spec[field] = value
    else if (Array.isArray(value)) spec[field] = value.filter((item): item is string => typeof item === 'string').join('\n')
    else if (typeof value === 'number' || typeof value === 'boolean') spec[field] = String(value)
  }
  return {
    roles: assignment?.roles ?? [],
    spec,
    enableApi: assignment?.spec?.enableApi === false ? false : true,
  }
}

/**
 * Whether the dashboard has dedicated settings fields for a kind. A kind without them gets the
 * form its module's spec schema generates (core-and-extensions.md §7), today NFS.
 */
export function hasDedicatedSoftwareSettings(entry: SoftwareCatalogEntry): boolean {
  return entry.kind === 'prometheus' || entry.kind === 'grafana' || entry.specFields.includes('version')
}

/** Maps an existing assignment to the explicit retry/reconfigure language used by the dialog. */
export function softwareInstallMode(assignment?: SoftwareAssignment): SoftwareInstallMode {
  if (assignment?.state === 'failed') return 'retry'
  if (assignment?.state === 'installed') return 'reconfigure'
  return 'install'
}

/** Visible action copy for the current install mode. */
export function softwareInstallActionLabel(mode: SoftwareInstallMode): string {
  if (mode === 'retry') return 'Retry install'
  if (mode === 'reconfigure') return 'Reconfigure'
  return 'Install'
}

/**
 * Builds the API-owned install payload from validated form state.
 *
 * The caller supplies the complete spec intended for every target. Reconfigure uses values
 * prefilled from its single assignment, preventing an omitted field from silently replacing the
 * persisted configuration with a default. A kind whose settings come from its module's spec schema
 * converts them by the schema's types.
 */
export function buildSoftwareInstallInput({
  entry,
  serverIds,
  roles,
  spec,
  enableApi,
  schema,
}: {
  entry: SoftwareCatalogEntry
  serverIds: readonly string[]
  roles: Readonly<Record<string, readonly SoftwareRole[]>>
  spec: Readonly<Record<string, string>>
  enableApi: boolean
  schema?: ModuleSpecSchema | null
}): InstallSoftwareInput {
  const normalizedSpec: Record<string, unknown> = schema ? moduleSettingsSpec(schema, spec) : {}
  for (const field of schema ? [] : entry.specFields) {
    if (field === 'enableApi') {
      normalizedSpec.enableApi = enableApi
      continue
    }
    if (SOFTWARE_LIST_SPEC_FIELDS.has(field)) {
      // The contract takes an array; commas, spaces, and line breaks all separate entries.
      const items = (spec[field] ?? '').split(/[\s,]+/).map((item) => item.trim()).filter(Boolean)
      if (items.length > 0) normalizedSpec[field] = items
      continue
    }
    const value = spec[field]?.trim()
    if (!value) continue
    // A non-numeric entry is sent as typed so the API's validation error explains it.
    normalizedSpec[field] = SOFTWARE_INTEGER_SPEC_FIELDS.has(field) && /^\d+$/.test(value) ? Number(value) : value
  }
  return {
    kind: entry.kind,
    assignments: serverIds.map((serverId) => ({ serverId, roles: [...(roles[serverId] ?? [])] })),
    ...(Object.keys(normalizedSpec).length > 0 ? { spec: normalizedSpec } : {}),
  }
}

/**
 * Whether a Prometheus retention is a duration Prometheus accepts, for example `15d` or `1w12h`.
 * Blank is valid: the API then keeps its 15-day default.
 */
export function isPrometheusRetentionValid(value: string): boolean {
  const trimmed = value.trim()
  return trimmed === '' || /^([0-9]+(y|w|d|h|m|s|ms))+$/.test(trimmed)
}

/** Mirrors contract-required role/spec fields for immediate, non-authoritative form feedback. */
export function isSoftwareInstallConfigurationValid({
  entry,
  serverIds,
  roles,
  spec,
}: {
  entry?: SoftwareCatalogEntry
  serverIds: readonly string[]
  roles: Readonly<Record<string, readonly SoftwareRole[]>>
  spec: Readonly<Record<string, string>>
}): boolean {
  if (!entry || serverIds.length === 0) return false
  if (entry.kind === 'prometheus' && !isPrometheusRetentionValid(spec.retention ?? '')) return false
  if (entry.roles.length === 0) return true
  if (!serverIds.every((serverId) => (roles[serverId]?.length ?? 0) > 0)) return false
  const anyServer = serverIds.some((serverId) => roles[serverId]?.includes('server'))
  const anyClient = serverIds.some((serverId) => roles[serverId]?.includes('client'))
  if (anyServer && !spec.exportPath?.trim()) return false
  if (anyClient && (!spec.source?.trim() || !spec.mountPath?.trim())) return false
  return true
}
