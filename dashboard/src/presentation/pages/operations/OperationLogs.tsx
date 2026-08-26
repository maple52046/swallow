import { useCallback, useEffect, useState } from 'react'
import { Box, Button, Flex, ScrollArea } from '@radix-ui/themes'
import { ReloadIcon } from '@radix-ui/react-icons'
import { useApp } from '@/di/AppProvider'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { EmptyState } from '@/presentation/components/EmptyState'

interface OperationLogsProps {
  operationId: string
}

type LogsState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; text: string }

/**
 * The retained runner log for one operation.
 *
 * Logs are plain text and can be large, so they are read on demand (when this tab mounts)
 * rather than polled, and rendered in a fixed-height scroll area with a manual refresh. The
 * backend guarantees credentials never appear in this text.
 */
export function OperationLogs({ operationId }: OperationLogsProps) {
  const { operations } = useApp()
  const [state, setState] = useState<LogsState>({ status: 'loading' })
  const [nonce, setNonce] = useState(0)
  const reload = useCallback(() => setNonce((value) => value + 1), [])

  useEffect(() => {
    let cancelled = false
    operations
      .getLogs(operationId)
      .then((text) => {
        if (!cancelled) setState({ status: 'ready', text })
      })
      .catch((err: Error) => {
        if (!cancelled) setState({ status: 'error', message: err.message })
      })
    return () => {
      cancelled = true
    }
  }, [operations, operationId, nonce])

  if (state.status === 'loading') return <LoadingState rows={4} />
  if (state.status === 'error') return <ErrorState message={state.message} onRetry={reload} />
  if (state.text.trim() === '') {
    return (
      <EmptyState
        title="No logs yet"
        message="Output appears once the run produces some; a pending run has none."
      />
    )
  }

  return (
    <Flex direction="column" gap="2">
      <Flex justify="end">
        <Button size="1" variant="soft" onClick={reload} aria-label="Refresh logs">
          <ReloadIcon />
          Refresh
        </Button>
      </Flex>
      <ScrollArea type="auto" scrollbars="both" style={{ maxHeight: 480 }}>
        <Box p="2" style={{ background: 'var(--gray-2)', borderRadius: 'var(--radius-2)' }}>
          <pre
            style={{
              fontFamily: 'monospace',
              fontSize: 'var(--font-size-1)',
              whiteSpace: 'pre',
              margin: 0,
            }}
          >
            {state.text}
          </pre>
        </Box>
      </ScrollArea>
    </Flex>
  )
}
