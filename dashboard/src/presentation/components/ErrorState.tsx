import { Alert, Button } from '@chakra-ui/react'

interface ErrorStateProps {
  message?: string
  onRetry?: () => void
}

/**
 * Shared actionable failure state for remote data.
 *
 * The concise message names what failed; when retry is safe, the inline button
 * keeps recovery beside the error without exposing transport implementation.
 */
export function ErrorState({ message, onRetry }: ErrorStateProps) {
  return (
    <Alert.Root status="error" variant="subtle" rounded="xl" borderWidth="1px">
      <Alert.Indicator />
      <Alert.Content>
        <Alert.Title>{message ?? 'Could not load this view'}</Alert.Title>
      </Alert.Content>
      {onRetry && (
        <Button size="sm" variant="outline" colorPalette="red" onClick={onRetry} alignSelf="center">
          Retry
        </Button>
      )}
    </Alert.Root>
  )
}
