import type { Server } from '@/domain/server/types'

/**
 * Why a Server is not offered as a Managed Software install target, in domain terms; the UI maps
 * each value to an explanation.
 *
 * - `absent`: the provisioner no longer reports the Server.
 * - `not_deployed`: no operating system is installed (the provisioning axis is not `deployed`).
 * - `deployment_running`: a swallow OS deployment is still deploying or verifying the Server.
 * - `deployment_unsuccessful`: the last swallow OS deployment failed, needs attention, or was
 *   canceled, so the installed OS is not one swallow could verify.
 */
export type SoftwareInstallBlocker = 'absent' | 'not_deployed' | 'deployment_running' | 'deployment_unsuccessful'

/**
 * The single rule for which Servers the dashboard offers as software install targets — the
 * Software page's target list and the Server detail page's Install software action both use it.
 *
 * A target needs an installed OS (provisioning `deployed`, not absent) and no swallow OS deployment
 * that is running or ended badly. A Server swallow never deployed (an existing Server, no
 * `deployment` record) qualifies: its OS is reachable once its default user is set. Returns null
 * when the Server qualifies. The API applies its own preconditions (deployed, unlocked, mutual
 * exclusion) and stays authoritative; this only keeps unusable Servers out of the choice.
 */
export function softwareInstallBlocker(server: Server): SoftwareInstallBlocker | null {
  if (server.absent) return 'absent'
  if (server.provisioning?.state !== 'deployed') return 'not_deployed'
  switch (server.deployment?.state) {
    case undefined:
    case 'succeeded':
      return null
    case 'deploying':
    case 'verifying':
      return 'deployment_running'
    default:
      return 'deployment_unsuccessful'
  }
}
