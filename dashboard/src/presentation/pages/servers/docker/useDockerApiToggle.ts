import { useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { SoftwareAssignment } from '@/domain/software/types'
import { useToast } from '@/presentation/components/toast/toastContext'

/**
 * Turns the Docker Engine API of an installed Docker CE on or off by re-running the same Managed
 * Software install with a changed `enableApi` (decision 043: no separate Job).
 *
 * The contract replaces the recorded spec on a re-apply, so the request sends the recorded spec with
 * only `enableApi` changed — a pinned `version` is kept. The request returns once the Workflow is
 * accepted; `onStarted` then re-reads the assignment, which turns `pending` and is polled until the
 * Workflow lands. A refusal (for example a locked Server or an active Workflow) is kept in `error`
 * for inline display. Re-applying restarts the Docker daemon on the host; callers must have shown
 * the shared risk notice first.
 */
export function useDockerApiToggle(serverId: string, assignment: SoftwareAssignment, onStarted: () => void) {
  const { software } = useApp()
  const { showToast } = useToast()
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const setApiEnabled = async (enabled: boolean): Promise<boolean> => {
    if (submitting) return false
    setSubmitting(true)
    setError('')
    try {
      await software.installSoftware({
        kind: 'docker-ce',
        assignments: [{ serverId, roles: [] }],
        spec: { ...(assignment.spec ?? {}), enableApi: enabled },
      })
      showToast({
        tone: 'info',
        title: enabled ? 'Enabling the Docker Engine API' : 'Disabling the Docker Engine API',
        description: 'swallow is re-applying Docker CE on this Server. This usually takes about a minute.',
      })
      onStarted()
      return true
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Docker CE could not be re-applied.')
      return false
    } finally {
      setSubmitting(false)
    }
  }

  return { submitting, error, setApiEnabled }
}
