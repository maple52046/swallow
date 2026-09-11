import { Alert, Button } from '@chakra-ui/react'
import { t } from '@/presentation/app/i18n'

interface ErrorStateProps {
  message?: string
  onRetry?: () => void
}

/**
 * Shared actionable failure state for remote data, distinct from empty and
 * unavailable. The message describes what failed; when `onRetry` is provided the
 * operator gets an inline retry rather than a dead end. Copy is localised through
 * the shared `t` helper.
 */
export function ErrorState({ message, onRetry }: ErrorStateProps) {
  return (
    <Alert.Root status="error">
      <Alert.Indicator />
      <Alert.Content>
        <Alert.Title>{message ?? t('error.loadFailed')}</Alert.Title>
      </Alert.Content>
      {onRetry && (
        <Button size="sm" variant="outline" colorPalette="red" onClick={onRetry} alignSelf="center">
          {t('error.tryAgain')}
        </Button>
      )}
    </Alert.Root>
  )
}
