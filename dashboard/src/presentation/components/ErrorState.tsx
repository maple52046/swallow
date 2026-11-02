import { Alert, Button } from '@chakra-ui/react'

interface ErrorStateProps {
  message?: string
  /** The request ID of the failed API call, shown so a user can quote it. */
  requestId?: string
  onRetry?: () => void
}

/**
 * Shared actionable failure state for remote data.
 *
 * The concise message names what failed and the request ID, when the API returned one, lets an
 * user quote the failure; when retry is safe, the inline button keeps recovery beside the
 * error without exposing transport implementation.
 */
export function ErrorState({ message, requestId, onRetry }: ErrorStateProps) {
  return (
    <Alert.Root status="error" variant="subtle" rounded="xl" borderWidth="1px">
      <Alert.Indicator />
      <Alert.Content>
        <Alert.Title>{message ?? 'Could not load this view'}</Alert.Title>
        {requestId && (
          <Alert.Description fontSize="xs" color="fg.muted" className="mono">
            Request ID: {requestId}
          </Alert.Description>
        )}
      </Alert.Content>
      {onRetry && (
        <Button size="sm" variant="outline" colorPalette="red" onClick={onRetry} alignSelf="center">
          Retry
        </Button>
      )}
    </Alert.Root>
  )
}
