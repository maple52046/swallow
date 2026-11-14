import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import type { Platform } from '@/domain/platform/types'
import { changePlanWaitDescription } from '@/presentation/components/changePlanWait'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useSlurmChangeWarnings, useSlurmPlatform } from './SlurmModuleProvider'
import type { SlurmTopologyChange } from './slurmTopology'

/** What a confirmed topology dialog tells the user once its write is accepted. */
export interface SlurmTopologyWriteCopy {
  /** The success toast's title, for example "Adding 2 nodes to mi300x". */
  started: string
  /** What happens next, for example that the new nodes are installed and validated. */
  detail: string
  /** The action the Change Plan approves when the write waits, for the wait toast ("remove the nodes"). */
  planAction: string
}

/**
 * The write lifecycle the Slurm workspace's topology dialogs share (platforms.md "Node groups and
 * partitions"): one in-flight change at a time, its failure kept for the open dialog to show (a
 * `conflict` stays in the dialog so the user can read why and retry, which re-reads the spec), and the
 * API's `warnings` kept (in the Slurm module provider, so the workspace's reload does not drop them)
 * until the user dismisses them, because they describe the Platform's nodes and should outlive a toast. A write whose Change Plan still waits (the Change Plan policy, or a blocker
 * such as busy partition) sends the user to Change Plans with the reason; otherwise `onChanged`
 * reloads the workspace, whose convergence polling follows the change.
 */
export function useSlurmTopologyWrite(platform: Platform, onChanged: () => void) {
  const slurm = useSlurmPlatform()
  const { showToast } = useToast()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)
  const [warnings, setWarnings] = useSlurmChangeWarnings(platform.id)

  /** Writes the change and resolves true when the API accepted it, so the caller closes its dialog. */
  const submit = async (change: SlurmTopologyChange, copy: SlurmTopologyWriteCopy): Promise<boolean> => {
    if (busy) return false
    setBusy(true)
    setError(null)
    try {
      const written = await slurm.changeTopology(platform.id, change)
      setWarnings(written.warnings)
      if (written.awaitingApproval) {
        showToast({ tone: 'warning', title: 'The change waits for approval', description: changePlanWaitDescription(written, copy.planAction) })
        navigate(scopedHref('/change-plans'))
        return true
      }
      showToast({ tone: 'success', title: copy.started, description: copy.detail })
      onChanged()
      return true
    } catch (caught) {
      setError(failureOf(caught, 'The change could not be started.'))
      return false
    } finally {
      setBusy(false)
    }
  }

  return { submit, busy, error, clearError: () => setError(null), warnings, dismissWarnings: () => setWarnings([]) }
}
