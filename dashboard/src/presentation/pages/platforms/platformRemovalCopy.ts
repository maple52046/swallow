/**
 * Operator-facing copy for the Platform removal actions (decision 010, 2026-10-29 amendment),
 * shared by the detail dialogs (`PlatformLifecycleActions`), the bulk dialogs, and the bulk toast
 * summary (`usePlatformBulkActions`) so they never describe the same choice differently.
 *
 * The wording keeps the glossary distinction visible: Uninstall changes the original deployment
 * targets, Delete removes only the Swallow record, and an uninstall with `deleteRecord` deletes the
 * record only after the uninstall succeeds. "Forget" is a deprecated term and is never used.
 */

/** The two choices that change what an uninstall request does. */
export interface PlatformUninstallChoice {
  /** Release every target to the provider instead of uninstalling the platform software. */
  releaseServers: boolean
  /** Also delete the record once the uninstall succeeds (`deleteRecord`). */
  deleteRecord: boolean
}

/** Tone, headline, and body of a consequence alert. */
export interface PlatformRemovalConsequence {
  status: 'warning' | 'error'
  title: string
  body: string
}

/**
 * The consequence of the Uninstall dialog's primary action (the record is kept). `scope` picks
 * singular wording for one Platform or plural wording for a bulk selection. The "… and delete
 * record" variant behind the submit chevron is explained by `platformUninstallDeleteHint`.
 */
export function platformUninstallConsequence(releaseServers: boolean, scope: 'single' | 'bulk'): PlatformRemovalConsequence {
  const bulk = scope === 'bulk'
  const targets = bulk ? 'each selected platform’s original deployment targets' : 'the original deployment targets'
  const releasedTargets = bulk ? 'every original deployment target of each selected platform' : 'every original deployment target'
  return releaseServers
    ? {
        status: 'error',
        title: 'Servers will be released',
        body: `Swallow will release ${releasedTargets} directly to its provider instead of uninstalling platform software. Releasing removes each deployed operating system. Disk erasure and static IP effects depend on the options below. This cannot be undone.`,
      }
    : {
        status: 'warning',
        title: 'Platform software will be removed',
        body: `Swallow will remove platform services, configuration, credentials, and managed state from ${targets}. The operating system, user data, and other installed packages remain. Hosts are not rebooted. This uninstall cannot be undone.`,
      }
}

/**
 * Explains, next to the typed confirmation, the record choice the Uninstall dialog's submit button
 * offers: the primary part keeps the record, and the variant in its chevron menu deletes it only
 * after the operation succeeds. Without this the variant would be discoverable only by opening
 * the menu.
 */
export function platformUninstallDeleteHint(releaseServers: boolean, scope: 'single' | 'bulk', count = 1): string {
  const primary = scope === 'bulk'
    ? platformBulkUninstallConfirmLabel({ releaseServers, deleteRecord: false }, count)
    : platformUninstallConfirmLabel({ releaseServers, deleteRecord: false })
  const variant = scope === 'bulk'
    ? platformBulkUninstallConfirmLabel({ releaseServers, deleteRecord: true }, count)
    : platformUninstallConfirmLabel({ releaseServers, deleteRecord: true })
  return scope === 'bulk'
    ? `“${primary}” keeps the Swallow records. To also delete them, choose “${variant}” from the arrow next to that button: each record is deleted only after that Platform’s operation succeeds, and a failed, canceled, or indeterminate operation keeps it.`
    : `“${primary}” keeps the Swallow record. To also delete it, choose “${variant}” from the arrow next to that button: the record is deleted only after the operation succeeds, and a failed, canceled, or indeterminate operation keeps it.`
}

/** Submit label of the single-Platform Uninstall dialog. */
export function platformUninstallConfirmLabel(choice: PlatformUninstallChoice): string {
  if (choice.deleteRecord) return choice.releaseServers ? 'Release servers and delete record' : 'Uninstall and delete record'
  return choice.releaseServers ? 'Release all servers' : 'Uninstall platform'
}

/** Submit label of the bulk Uninstall dialog for `count` targeted Platforms. */
export function platformBulkUninstallConfirmLabel(choice: PlatformUninstallChoice, count: number): string {
  if (choice.deleteRecord) {
    const records = plural(count, 'record')
    return choice.releaseServers ? `Release servers, then delete ${count} ${records}` : `Uninstall, then delete ${count} ${records}`
  }
  const platforms = plural(count, 'platform')
  return choice.releaseServers ? `Release servers for ${count} ${platforms}` : `Uninstall software from ${count} ${platforms}`
}

/**
 * Accessible name of the checkbox that consents to the backend canceling blocking Workflows. It
 * names only the host-side action, because the record choice is made later, on submit.
 */
export function platformBlockingWorkflowsLabel(releaseServers: boolean, scope: 'single' | 'bulk'): string {
  const bulk = scope === 'bulk'
  const action = releaseServers
    ? bulk ? 'release the selected platforms’ servers' : 'release the servers'
    : bulk ? 'uninstall the selected platforms' : 'uninstall'
  return `Cancel these workflows, then ${action}`
}

/**
 * Success toast after the single-Platform uninstall request was accepted. The record is only
 * deleted later, when the uninstall succeeds, so the copy promises nothing about a failed run.
 */
export function platformUninstallAcceptedToast(choice: PlatformUninstallChoice, targets: string): { title: string; description: string } {
  const released = `${capitalize(targets)} will be released directly to the provider.`
  if (choice.deleteRecord) {
    return choice.releaseServers
      ? { title: 'Release and delete accepted', description: `${released} After every release succeeds, Swallow will delete the record; if the operation fails, the record will remain.` }
      : { title: 'Uninstall and delete accepted', description: `Platform software will be removed from ${targets}. After the uninstall succeeds, Swallow will delete the record; if it fails, the record will remain.` }
  }
  return choice.releaseServers
    ? { title: 'Server release accepted', description: `${released} Platform software uninstall is skipped, and the Swallow record will be retained.` }
    : { title: 'Platform uninstall accepted', description: `Platform software will be removed from ${targets}. The Swallow record will be retained.` }
}

/**
 * Why a record-only Delete is being confirmed, which decides its warning:
 *
 * - `installed`: a Swallow-deployed Platform whose software may still run on the hosts, reached
 *   through "Delete only…"; the warning points to Uninstall for host cleanup.
 * - `uninstalled`: the hosts were already cleaned; only the record is left.
 * - `external`: Swallow did not deploy it and cannot uninstall it.
 */
export type PlatformRecordDeleteContext = 'installed' | 'uninstalled' | 'external'

/** Consequence of the single-Platform record-only Delete for `context`. */
export function platformRecordDeleteConsequence(context: PlatformRecordDeleteContext): PlatformRemovalConsequence {
  if (context === 'uninstalled') {
    return {
      status: 'error',
      title: 'Delete the remaining record',
      body: 'Platform software has already been removed from the original deployment targets. Swallow will permanently delete this record and its owned projections. Hosts will not be changed. This cannot be undone.',
    }
  }
  if (context === 'external') {
    return {
      status: 'error',
      title: 'Hosts will not be changed',
      body: 'This Platform was not deployed by Swallow, so Swallow cannot uninstall it. Swallow will cancel its in-flight workflows, then permanently delete the record and owned projections. Any Platform still running on the hosts keeps running. This cannot be undone.',
    }
  }
  return {
    status: 'error',
    title: 'Hosts will not be cleaned up',
    body: 'Swallow will cancel this Platform’s in-flight workflows, then permanently delete the record and owned projections. It will not uninstall platform software or release servers; any software still running on the hosts keeps running. If you want host cleanup, cancel this dialog and use Uninstall platform instead; if an uninstall is already running, let it finish. This cannot be undone.',
  }
}

/** Consequence of the bulk record-only Delete, which targets every selected Platform. */
export const PLATFORM_BULK_RECORD_DELETE_CONSEQUENCE: PlatformRemovalConsequence = {
  status: 'error',
  title: 'Hosts will not be changed',
  body: 'Swallow will cancel each selected Platform’s in-flight workflows, then permanently delete its record and owned projections. Any Platform software still running on hosts keeps running. This cannot be undone.',
}

/**
 * Toast title and description after every bulk uninstall request was accepted, or the
 * partial-failure title when some were refused. Each request is independent, so with
 * `deleteRecord` a record is still deleted only after its own uninstall succeeds.
 */
export function platformBulkUninstallToast(choice: PlatformUninstallChoice, accepted: number): { title: string; partialTitle: string; description: string } {
  const requests = plural(accepted, 'request')
  if (!choice.deleteRecord) {
    return choice.releaseServers
      ? {
          title: 'Bulk server release accepted',
          partialTitle: 'Bulk server release partially accepted',
          description: `${accepted} release ${requests} accepted. Servers will be released directly; the Swallow records will be retained.`,
        }
      : {
          title: 'Bulk uninstall accepted',
          partialTitle: 'Bulk uninstall partially accepted',
          description: `${accepted} uninstall ${requests} accepted. Platform software will be removed; the Swallow records will be retained.`,
        }
  }
  const step = choice.releaseServers ? 'release' : 'uninstall'
  return {
    title: choice.releaseServers ? 'Bulk release and delete accepted' : 'Bulk uninstall and delete accepted',
    partialTitle: choice.releaseServers ? 'Bulk release and delete partially accepted' : 'Bulk uninstall and delete partially accepted',
    description: `${accepted} ${step} ${requests} accepted. Each record will be deleted only after its ${step} succeeds; a failed ${step} keeps its record.`,
  }
}

/** Toast copy after a bulk record-only Delete with `accepted` removed records. */
export function platformBulkDeleteToast(accepted: number): { title: string; partialTitle: string; description: string } {
  return {
    title: 'Platform records deleted',
    partialTitle: 'Platform records partially deleted',
    description: `${accepted} Swallow ${plural(accepted, 'record')} and owned projections were removed. Hosts were not changed.`,
  }
}

/** `word` pluralised with a trailing "s" for any count other than one. */
export function plural(count: number, word: string): string {
  return count === 1 ? word : `${word}s`
}

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1)
}
