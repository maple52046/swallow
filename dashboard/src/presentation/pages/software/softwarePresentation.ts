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

/** The rule the install dialog's target list states under its heading. */
export const SOFTWARE_TARGETS_HINT =
  'Deployed Servers are listed unless an OS deployment on them is running, failed, or needs attention.'

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
