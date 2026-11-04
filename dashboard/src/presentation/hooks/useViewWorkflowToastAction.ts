import { useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import type { ToastOptions } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'

/** Long enough to read the toast and reach its button; the default success dwell is not. */
const WORKFLOW_TOAST_DURATION_MS = 12_000

/**
 * Returns a builder for the "View workflow" part of a success toast shown after the API accepted
 * durable work and answered with its Workflow id (for example the `operationId` of an OS deployment,
 * Release, or Recover; provisioning.md "Durable OS Workflows"). Spread the result into the toast's
 * options: it sets the action and a dwell time long enough to use it.
 *
 * The link keeps a Site scope: `siteId` when the caller knows the Site the Workflow's targets belong
 * to (so a toast raised in the all-Sites view still opens the Workflow scoped), otherwise the page's
 * current scope. The toast renders the action as a keyboard-reachable button; navigation happens
 * only when the user activates it, and nothing is fetched here.
 */
export function useViewWorkflowToastAction(): (workflowId: string, siteId?: string) => Pick<ToastOptions, 'action' | 'duration'> {
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  return useCallback(
    (workflowId: string, siteId?: string) => ({
      duration: WORKFLOW_TOAST_DURATION_MS,
      action: {
        label: 'View workflow',
        onClick: () => navigate(scopedHref(`/workflows/${encodeURIComponent(workflowId)}`, siteId)),
      },
    }),
    [navigate, scopedHref],
  )
}
