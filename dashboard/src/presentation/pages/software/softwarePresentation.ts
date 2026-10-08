import type { SoftwareInstallBlocker } from '@/domain/software/targets'
import type { SoftwareAssignmentState, SoftwareKind } from '@/domain/software/types'

/**
 * Why a Server cannot take a software install, as shown beside the disabled Install software
 * action on its detail page. One sentence per domain blocker so the reason reads the same wherever
 * it appears.
 */
export function softwareInstallBlockerReason(blocker: SoftwareInstallBlocker): string {
  switch (blocker) {
    case 'absent':
      return 'The provisioner no longer reports this Server.'
    case 'not_deployed':
      return 'Install an operating system on this Server first.'
    case 'deployment_running':
      return 'Wait for the OS deployment to finish.'
    case 'deployment_unsuccessful':
      return 'The last OS deployment did not succeed. Retry or redeploy it first.'
  }
}

/** Legacy query parameter retained for old `/software/settings` bookmarks. */
export const SOFTWARE_SETTINGS_KIND_PARAM = 'kind'

/**
 * Canonical settings path for one software kind. Callers add the Site scope with `scopedHref`.
 */
export function softwareSettingsPath(kind: SoftwareKind): string {
  return `/software/${encodeURIComponent(kind)}/settings`
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
      return 'Installing'
    case 'uninstalling':
      return 'Uninstalling'
    case 'absent':
      return 'Absent'
    default:
      return state
  }
}
