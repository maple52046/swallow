import { useCallback, useState } from 'react'
import { useToast } from '@/presentation/components/toast/toastContext'

/** Operator-facing outcome messages for one Docker write. */
export interface DockerMutationMessages {
  success: string
  failure: string
  /** Names the target (image tag, container name) in the success toast. */
  target?: string
}

/**
 * Runs Docker Host Explorer writes with consistent feedback.
 *
 * Each write reports success or the API's message through the shared toast channel, then calls
 * `onSuccess` (the section reloads its list and the explorer refreshes its counts). `busyKey`
 * identifies the one write in flight — typically `<action>:<object id>` — so the triggering control
 * can show progress and repeated clicks cannot start a second identical request. `run` resolves to
 * whether the write succeeded so a dialog can decide to close.
 */
export function useDockerMutation(onSuccess: () => void) {
  const { showToast } = useToast()
  const [busyKey, setBusyKey] = useState<string | null>(null)

  const run = useCallback(
    async (key: string, action: () => Promise<unknown>, messages: DockerMutationMessages): Promise<boolean> => {
      setBusyKey(key)
      try {
        await action()
        showToast({ tone: 'success', title: messages.success, description: messages.target })
        onSuccess()
        return true
      } catch (error) {
        showToast({ tone: 'error', title: messages.failure, description: error instanceof Error ? error.message : 'The Docker Engine request failed.' })
        return false
      } finally {
        setBusyKey(null)
      }
    },
    [showToast, onSuccess],
  )

  return { busyKey, run }
}
