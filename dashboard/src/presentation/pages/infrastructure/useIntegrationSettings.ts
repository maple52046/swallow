import { useCallback, useEffect, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { Integration } from '@/domain/site/types'

/** The General tab's editable fields while they differ from the saved Integration. */
export interface IntegrationGeneralDraft {
  name: string
  endpoint: string
  enabled: boolean
}

/**
 * What the Integration settings page hands its tabs through the router outlet. Drafts live on the
 * page, not in the tabs, so switching tabs keeps unsaved edits; they are dropped only when the page
 * unmounts or a save succeeds. A draft is `null` while the tab shows the saved values.
 */
export interface IntegrationSettingsContext {
  integration: Integration
  /** The Site's display name, or its id while the Site list is still loading. */
  siteName: string
  generalDraft: IntegrationGeneralDraft | null
  setGeneralDraft: (draft: IntegrationGeneralDraft | null) => void
  connectionDraft: Record<string, string> | null
  setConnectionDraft: (draft: Record<string, string> | null) => void
  /** Replaces the page's Integration with a value the API returned, e.g. after a PATCH. */
  onSaved: (integration: Integration) => void
  /** Reads the Integration again, e.g. after a credential write that returns no Integration. */
  reload: () => void
}

/** Reads the settings page's outlet context; only the page's tab routes render under it. */
export function useIntegrationSettingsContext(): IntegrationSettingsContext {
  return useOutletContext<IntegrationSettingsContext>()
}

/** The settings page's view of its Integration read: loading, missing, failed, or loaded. */
export type IntegrationLoadState =
  | { status: 'loading' }
  | { status: 'not-found' }
  | { status: 'error'; message: string }
  | { status: 'ready'; integration: Integration }

/**
 * Loads one Integration for its settings page and keeps it current: `reload` reads it again, and
 * `replace` swaps in a value the API already returned so a save needs no second request. A stale
 * response for a previous id (or a superseded reload) is dropped. An unknown id is `not-found`.
 */
export function useIntegration(id: string | undefined): {
  state: IntegrationLoadState
  reload: () => void
  replace: (integration: Integration) => void
} {
  const { sites } = useApp()
  const [state, setState] = useState<IntegrationLoadState>(id ? { status: 'loading' } : { status: 'not-found' })
  const [nonce, setNonce] = useState(0)

  useEffect(() => {
    if (!id) return
    let cancelled = false
    sites
      .getIntegration(id)
      .then((integration) => {
        if (cancelled) return
        setState(integration ? { status: 'ready', integration } : { status: 'not-found' })
      })
      .catch((caught: Error) => {
        if (!cancelled) setState({ status: 'error', message: caught.message })
      })
    return () => {
      cancelled = true
    }
  }, [sites, id, nonce])

  const reload = useCallback(() => setNonce((value) => value + 1), [])
  const replace = useCallback((integration: Integration) => setState({ status: 'ready', integration }), [])
  return { state, reload, replace }
}
