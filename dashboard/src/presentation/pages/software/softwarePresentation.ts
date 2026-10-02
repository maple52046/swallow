import type { SoftwareAssignmentState, SoftwareKind } from '@/domain/software/types'

/** The query parameter of `/software/settings` that selects one software kind's settings group. */
export const SOFTWARE_SETTINGS_KIND_PARAM = 'kind'

/**
 * Path of one kind's group on the Software settings page, for links from elsewhere (for example the
 * Pull image dialog pointing at Docker CE's Registry credentials). Callers add the Site scope with
 * `scopedHref`.
 */
export function softwareSettingsPath(kind: SoftwareKind): string {
  return `/software/settings?${SOFTWARE_SETTINGS_KIND_PARAM}=${encodeURIComponent(kind)}`
}

/**
 * Presentation helpers shared by the Software page and its dialogs so the assignment state and
 * software-kind labels are rendered identically everywhere (DRY). These map domain values to
 * operator-facing text and a badge palette; the palette only reinforces the text, which always
 * carries the meaning so state is never conveyed by colour alone.
 */

/** Fallback label for a kind not covered by the fetched catalog (a forward-compatible value). */
export function softwareKindLabel(kind: SoftwareKind, catalogLabel?: string): string {
  if (catalogLabel) return catalogLabel
  switch (kind) {
    case 'docker-ce':
      return 'Docker CE'
    case 'podman':
      return 'Podman'
    case 'nfs':
      return 'NFS'
    default:
      return kind
  }
}

/** The Chakra `colorPalette` reinforcing an assignment state; text still carries the meaning. */
export function assignmentStatePalette(state: SoftwareAssignmentState): string {
  switch (state) {
    case 'installed':
      return 'green'
    case 'failed':
      return 'red'
    case 'pending':
    case 'uninstalling':
      return 'yellow'
    case 'absent':
    default:
      return 'gray'
  }
}

/** Operator-facing label for an assignment state. */
export function assignmentStateLabel(state: SoftwareAssignmentState): string {
  switch (state) {
    case 'installed':
      return 'Installed'
    case 'failed':
      return 'Failed'
    case 'pending':
      return 'Pending'
    case 'uninstalling':
      return 'Uninstalling'
    case 'absent':
      return 'Absent'
    default:
      return state
  }
}
