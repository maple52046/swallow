import { useCallback, useEffect, useState } from 'react'
import { Alert, AlertVariant, Button, Toolbar, ToolbarContent, ToolbarItem } from '@patternfly/react-core'
import { CopyIcon, DownloadIcon, SyncAltIcon } from '@patternfly/react-icons'
import { LogViewer, LogViewerSearch } from '@patternfly/react-log-viewer'
import { useApp } from '@/di/AppProvider'
import { useAppearance } from '@/presentation/app/theme/appearanceContext'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'

type LogsState = { status: 'loading' } | { status: 'error'; message: string } | { status: 'ready'; text: string }
type CopyFeedback = { title: string; variant: AlertVariant } | null

/**
 * PatternFly retained-stdout workspace with built-in match count/navigation plus copy,
 * refresh, and client-side download. Blob URLs are revoked immediately after dispatch.
 */
export function OperationLogWorkspace({ operationId }: { operationId: string }) {
  const { operations } = useApp()
  const { resolved } = useAppearance()
  const [state, setState] = useState<LogsState>({ status: 'loading' })
  const [copyFeedback, setCopyFeedback] = useState<CopyFeedback>(null)
  const [nonce, setNonce] = useState(0)
  const reload = useCallback(() => setNonce((value) => value + 1), [])
  useEffect(() => {
    let cancelled = false
    operations.getLogs(operationId).then((text) => { if (!cancelled) setState({ status: 'ready', text }) }).catch((error: Error) => { if (!cancelled) setState({ status: 'error', message: error.message }) })
    return () => { cancelled = true }
  }, [nonce, operationId, operations])
  if (state.status === 'loading') return <LoadingState rows={5} />
  if (state.status === 'error') return <ErrorState message={state.message} onRetry={reload} />
  if (!state.text.trim()) return <EmptyState title="No stdout yet" message="Output appears after the run starts producing stdout." />

  const copy = async () => {
    setCopyFeedback({ title: 'Stdout copied', variant: AlertVariant.success })
    try {
      await navigator.clipboard.writeText(state.text)
    } catch {
      setCopyFeedback({ title: 'Could not copy stdout. Clipboard access is unavailable in this browser.', variant: AlertVariant.danger })
    }
  }
  const download = () => {
    const url = URL.createObjectURL(new Blob([state.text], { type: 'text/plain;charset=utf-8' }))
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `swallow-operation-${operationId}.log`
    anchor.click()
    URL.revokeObjectURL(url)
  }
  const toolbar = <Toolbar><ToolbarContent><ToolbarItem variant="label"><LogViewerSearch placeholder="Search stdout" minSearchChars={1} /></ToolbarItem><ToolbarItem align={{ default: 'alignEnd' }}><Button variant="plain" icon={<CopyIcon />} aria-label="Copy stdout" onClick={() => void copy()} /></ToolbarItem><ToolbarItem><Button variant="plain" icon={<DownloadIcon />} aria-label="Download stdout" onClick={download} /></ToolbarItem><ToolbarItem><Button variant="plain" icon={<SyncAltIcon />} aria-label="Refresh stdout" onClick={reload} /></ToolbarItem></ToolbarContent></Toolbar>
  return <>{copyFeedback && <Alert variant={copyFeedback.variant} title={copyFeedback.title} isInline />}<LogViewer data={state.text} hasLineNumbers height={520} theme={resolved} toolbar={toolbar} aria-label="Operation stdout" /></>
}
