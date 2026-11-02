/**
 * User-facing copy for the Platform removal actions (decision 010, 2026-10-29 amendment),
 * shared by the detail dialogs (`PlatformLifecycleActions`), the bulk dialogs, and the bulk toast
 * summary (`usePlatformBulkActions`) so they never describe the same choice differently.
 *
 * The wording keeps the glossary distinction visible: Uninstall changes the original deployment
 * targets and waits on a Change Plan, after which the record is deleted, while "Delete only"
 * removes only the swallow record. "Forget" is a deprecated term and is never used.
 */

/** The choice that changes what an uninstall does. */
export interface PlatformUninstallChoice {
  /** Release every target to the provider instead of uninstalling the platform software. */
  releaseServers: boolean
}

/** Tone, headline, and body of a consequence alert. */
export interface PlatformRemovalConsequence {
  status: 'warning' | 'error'
  title: string
  body: string
}

/**
 * The consequence of the Uninstall dialog's action. `scope` picks singular wording for one Platform
 * or plural wording for a bulk selection. When the uninstall runs, and that the record follows, is
 * explained by `platformUninstallDeleteHint`.
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
 * Explains, next to the typed confirmation, that nothing changes until the Change Plan is
 * approved and that the record is deleted once the uninstall succeeds.
 */
export function platformUninstallDeleteHint(releaseServers: boolean, scope: 'single' | 'bulk'): string {
  const step = releaseServers ? 'release' : 'uninstall'
  return scope === 'bulk'
    ? `Each platform’s ${step} waits on a Change Plan you approve in Change Plans. Each Swallow record is deleted once that platform’s ${step} succeeds; a failed ${step} keeps it.`
    : `The ${step} waits on a Change Plan you approve in Change Plans. The Swallow record is deleted once the ${step} succeeds; a failed ${step} keeps it.`
}

/** Submit label of the single-Platform Uninstall dialog. */
export function platformUninstallConfirmLabel(choice: PlatformUninstallChoice): string {
  return choice.releaseServers ? 'Propose server release' : 'Propose uninstall'
}

/** Submit label of the bulk Uninstall dialog for `count` targeted Platforms. */
export function platformBulkUninstallConfirmLabel(choice: PlatformUninstallChoice, count: number): string {
  const platforms = plural(count, 'platform')
  return choice.releaseServers ? `Propose server release for ${count} ${platforms}` : `Propose uninstall for ${count} ${platforms}`
}

/** Accessible name of the checkbox that consents to the backend canceling blocking Workflows. */
export function platformBlockingWorkflowsLabel(releaseServers: boolean, scope: 'single' | 'bulk'): string {
  const bulk = scope === 'bulk'
  const action = releaseServers
    ? bulk ? 'release the selected platforms’ servers' : 'release the servers'
    : bulk ? 'uninstall the selected platforms' : 'uninstall'
  return `Cancel these workflows, then ${action}`
}

/**
 * Success toast after the single-Platform uninstall was proposed. Nothing changes until the plan
 * is approved, and the record is deleted only when the uninstall succeeds.
 */
export function platformUninstallAcceptedToast(choice: PlatformUninstallChoice, targets: string): { title: string; description: string } {
  return choice.releaseServers
    ? { title: 'Server release proposed', description: `Approve the Change Plan to release ${targets} directly to the provider. The Swallow record is deleted after every release succeeds.` }
    : { title: 'Uninstall proposed', description: `Approve the Change Plan to remove platform software from ${targets}. The Swallow record is deleted after the uninstall succeeds.` }
}

/**
 * Why a record-only Delete is being confirmed, which decides its warning:
 *
 * - `installed`: a swallow-deployed Platform whose software may still run on the hosts, reached
 *   through "Delete only…"; the warning points to Uninstall for host cleanup.
 * - `uninstalled`: the hosts were already cleaned; only the record is left.
 * - `external`: swallow did not deploy it and cannot uninstall it.
 * - `planned`: its install still waits on its Change Plan, so nothing was installed.
 */
export type PlatformRecordDeleteContext = 'installed' | 'uninstalled' | 'external' | 'planned'

/** Consequence of the single-Platform record-only Delete for `context`. */
export function platformRecordDeleteConsequence(context: PlatformRecordDeleteContext): PlatformRemovalConsequence {
  if (context === 'planned') {
    return {
      status: 'warning',
      title: 'Nothing was installed',
      body: 'This Platform’s install still waits on its Change Plan. Swallow will withdraw that plan, release the Servers it claimed, and delete the record. Hosts were never changed.',
    }
  }
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
 * Toast title and description after every bulk uninstall was proposed, or the partial-failure
 * title when some were refused. Each platform gets its own Change Plan.
 */
export function platformBulkUninstallToast(choice: PlatformUninstallChoice, accepted: number): { title: string; partialTitle: string; description: string } {
  const plans = plural(accepted, 'Change Plan')
  return choice.releaseServers
    ? {
        title: 'Bulk server release proposed',
        partialTitle: 'Bulk server release partially proposed',
        description: `${accepted} ${plans} proposed. Approve each in Change Plans to release its servers; each record is deleted after its release succeeds.`,
      }
    : {
        title: 'Bulk uninstall proposed',
        partialTitle: 'Bulk uninstall partially proposed',
        description: `${accepted} ${plans} proposed. Approve each in Change Plans to remove its platform software; each record is deleted after its uninstall succeeds.`,
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
