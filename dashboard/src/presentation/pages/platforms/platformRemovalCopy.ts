/**
 * User-facing copy for the Platform removal actions (decision 010, 2026-10-29 amendment),
 * shared by the detail dialogs (`PlatformLifecycleActions`), the bulk dialogs, and the bulk toast
 * summary (`usePlatformBulkActions`) so they never describe the same choice differently. Every piece
 * of Uninstall copy follows the release choice, from the dialog title to the toast.
 *
 * The wording keeps the glossary distinction visible: Uninstall changes the original deployment
 * targets through a Change Plan, which the confirmed dialog approves with its request unless the
 * Change Plan policy requires a separate approval, after which the record is deleted; "Delete only"
 * removes only the swallow record. "Forget" is a deprecated term and is never used.
 */

import { changePlanWaitDescription } from '@/presentation/components/changePlanWait'
import type { PlatformChangeResult } from '@/domain/platform/types'
import { plural } from '@/shared/utils/plural'

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
 * Explains, next to the typed confirmation, that confirming approves the Change Plan and starts the
 * uninstall (unless the Change Plan policy requires a separate approval), and that the record is
 * deleted once the uninstall succeeds.
 */
export function platformUninstallDeleteHint(releaseServers: boolean, scope: 'single' | 'bulk'): string {
  const step = releaseServers ? 'release' : 'uninstall'
  return scope === 'bulk'
    ? `Confirming approves each platform’s ${step} and starts it, unless the Change Plan policy requires a separate approval in Change Plans. Each Swallow record is deleted once that platform’s ${step} succeeds; a failed ${step} keeps it.`
    : `Confirming approves the ${step} and starts it, unless the Change Plan policy requires a separate approval in Change Plans. The Swallow record is deleted once the ${step} succeeds; a failed ${step} keeps it.`
}

/** Heading and introduction of an Uninstall dialog. */
export interface PlatformUninstallDialogCopy {
  title: string
  description: string
}

/** The bulk dialog's copy: its heading and intro, and the label of the list of targeted Platforms. */
export interface PlatformBulkUninstallDialogCopy extends PlatformUninstallDialogCopy {
  listLabel: string
}

/**
 * Title and intro of the single-Platform Uninstall dialog for `platformName`. Checking "Release all
 * servers instead of uninstalling platform software" makes the proposed action a release of every
 * original deployment target (platforms.md "Delete A Platform": the software removal is skipped), so
 * the heading and intro then describe a release rather than a software uninstall.
 */
export function platformUninstallDialogCopy(choice: PlatformUninstallChoice, platformName: string): PlatformUninstallDialogCopy {
  return choice.releaseServers
    ? {
        title: 'Release platform servers',
        description: `Release every server of “${platformName}” to its provisioner instead of uninstalling platform software. The original deployment targets are released, and the Swallow record is deleted after the release succeeds.`,
      }
    : {
        title: 'Uninstall platform',
        description: `Remove Swallow-managed platform software from “${platformName}”. The uninstall acts on the original deployment targets, and the Swallow record is deleted after it succeeds.`,
      }
}

/**
 * Title, intro, and target-list label of the bulk Uninstall dialog; the release choice changes them
 * as in the single dialog. The list label is also the list's accessible name.
 */
export function platformBulkUninstallDialogCopy(choice: PlatformUninstallChoice): PlatformBulkUninstallDialogCopy {
  return choice.releaseServers
    ? {
        title: 'Release servers of selected platforms',
        description: 'Release the servers of selected Platforms that are currently eligible to their provisioner instead of uninstalling platform software; each gets its own Change Plan. Ineligible selections will be skipped.',
        listLabel: 'Platforms whose servers are released',
      }
    : {
        title: 'Uninstall selected platforms',
        description: 'Uninstall platform software from selected Platforms that are currently eligible; each gets its own Change Plan. Ineligible selections will be skipped.',
        listLabel: 'Platforms to uninstall',
      }
}

/** Submit label of the single-Platform Uninstall dialog. */
export function platformUninstallConfirmLabel(choice: PlatformUninstallChoice): string {
  return choice.releaseServers ? 'Release servers' : 'Uninstall'
}

/** Submit label of the bulk Uninstall dialog for `count` targeted Platforms. */
export function platformBulkUninstallConfirmLabel(choice: PlatformUninstallChoice, count: number): string {
  const platforms = plural(count, 'platform')
  return choice.releaseServers ? `Release servers of ${count} ${platforms}` : `Uninstall ${count} ${platforms}`
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
 * Toast after the single-Platform uninstall was accepted: started, because the confirmed dialog
 * approved its Change Plan, or waiting for a separate approval (the Change Plan policy or a
 * blocker), in which case it says why. The record is deleted only when the uninstall succeeds.
 */
export function platformUninstallAcceptedToast(
  choice: PlatformUninstallChoice,
  targets: string,
  result: Pick<PlatformChangeResult, 'awaitingApproval' | 'waitReason' | 'blocked'>,
): { tone: 'success' | 'warning'; title: string; description: string } {
  if (result.awaitingApproval) {
    return choice.releaseServers
      ? { tone: 'warning', title: 'Server release waits for approval', description: changePlanWaitDescription(result, `release ${targets} to the provider`) }
      : { tone: 'warning', title: 'Uninstall waits for approval', description: changePlanWaitDescription(result, `remove platform software from ${targets}`) }
  }
  return choice.releaseServers
    ? { tone: 'success', title: 'Server release started', description: `Swallow is releasing ${targets} directly to the provider. The Swallow record is deleted after every release succeeds.` }
    : { tone: 'success', title: 'Uninstall started', description: `Swallow is removing platform software from ${targets}. The Swallow record is deleted after the uninstall succeeds.` }
}

/**
 * Why a record-only Delete is being confirmed, which decides its warning:
 *
 * - `installed`: a swallow-deployed Platform whose software may still run on the hosts, reached
 *   through "Delete only…"; the warning points to Uninstall for host cleanup.
 * - `uninstalled`: the hosts were already cleaned; only the record is left.
 * - `external`: swallow did not deploy it and cannot uninstall it.
 * - `planned`: its install has not started, so nothing was installed.
 */
export type PlatformRecordDeleteContext = 'installed' | 'uninstalled' | 'external' | 'planned'

/** Consequence of the single-Platform record-only Delete for `context`. */
export function platformRecordDeleteConsequence(context: PlatformRecordDeleteContext): PlatformRemovalConsequence {
  if (context === 'planned') {
    return {
      status: 'warning',
      title: 'Nothing was installed',
      body: 'This Platform’s install has not started. Swallow will withdraw its Change Plan, release the Servers it claimed, and delete the record. Hosts were never changed.',
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

/** How a bulk uninstall's accepted Platforms went, counted from their outcomes. */
export interface PlatformBulkUninstallCounts {
  /** Uninstalls whose Change Plan the confirmed dialog approved, so they started. */
  started: number
  /** Uninstalls whose Change Plan waits for a separate approval (the policy or a blocker). */
  waiting: number
  /** Platforms with nothing installed (their install never started), which the API deleted at once. */
  deletedAtOnce: number
  /** Platforms whose uninstall was already running, so the request changed nothing. */
  alreadyRunning: number
}

/**
 * Toast title and description after a bulk uninstall, or the partial-failure title when some
 * requests were refused. The copy names only what happened: uninstalls that started, plans that
 * wait in Change Plans (and that the user is sent there for them), Platforms deleted at once, and
 * Platforms whose uninstall was already running, so a run where nothing waits does not send the
 * user to approve anything.
 */
export function platformBulkUninstallToast(
  choice: PlatformUninstallChoice,
  counts: PlatformBulkUninstallCounts,
): { title: string; partialTitle: string; description: string } {
  const { started, waiting, deletedAtOnce, alreadyRunning } = counts
  const step = choice.releaseServers ? 'release' : 'uninstall'
  const notes = [
    started > 0 ? `${started} ${plural(started, step)} started; each record is deleted after its ${step} succeeds.` : '',
    waiting > 0 ? `${waiting} ${plural(waiting, step)} ${waiting === 1 ? 'waits' : 'wait'} for approval in Change Plans.` : '',
    deletedAtOnce > 0
      ? `${deletedAtOnce} ${plural(deletedAtOnce, 'Platform')} whose install never started ${deletedAtOnce === 1 ? 'was' : 'were'} deleted at once; no host was changed.`
      : '',
    alreadyRunning > 0
      ? `${alreadyRunning} ${plural(alreadyRunning, 'Platform')} ${alreadyRunning === 1 ? 'was' : 'were'} already being uninstalled; nothing changed for ${alreadyRunning === 1 ? 'it' : 'them'}.`
      : '',
  ].filter(Boolean)
  const description = notes.join(' ')
  if (started === 0 && waiting === 0 && deletedAtOnce === 0) {
    return { title: 'Uninstall already running', partialTitle: 'Uninstall partially requested', description }
  }
  if (started === 0 && waiting === 0) {
    return { title: 'Platforms deleted', partialTitle: 'Platforms partially deleted', description }
  }
  if (started === 0) {
    return choice.releaseServers
      ? { title: 'Server release waits for approval', partialTitle: 'Server release partially requested', description }
      : { title: 'Uninstall waits for approval', partialTitle: 'Uninstall partially requested', description }
  }
  return choice.releaseServers
    ? { title: 'Bulk server release started', partialTitle: 'Bulk server release partially started', description }
    : { title: 'Bulk uninstall started', partialTitle: 'Bulk uninstall partially started', description }
}

/** Toast copy after a bulk record-only Delete with `accepted` removed records. */
export function platformBulkDeleteToast(accepted: number): { title: string; partialTitle: string; description: string } {
  return {
    title: 'Platform records deleted',
    partialTitle: 'Platform records partially deleted',
    description: `${accepted} Swallow ${plural(accepted, 'record')} and owned projections were removed. Hosts were not changed.`,
  }
}