import { Alert, AlertActionLink, AlertVariant } from '@patternfly/react-core'
import { t } from '@/presentation/app/i18n'

interface ErrorStateProps {
  message?: string
  onRetry?: () => void
}

/** Shared actionable failure state for remote data, distinct from empty and unavailable. */
export function ErrorState({ message, onRetry }: ErrorStateProps) {
  return (
    <Alert
      variant={AlertVariant.danger}
      title={message ?? t('error.loadFailed')}
      actionLinks={onRetry ? <AlertActionLink onClick={onRetry}>{t('error.tryAgain')}</AlertActionLink> : undefined}
      isInline
    />
  )
}
