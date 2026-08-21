import { Button, Callout, Flex } from '@radix-ui/themes'
import { ExclamationTriangleIcon, ReloadIcon } from '@radix-ui/react-icons'
import { t } from '@/presentation/app/i18n'

interface ErrorStateProps {
  message?: string
  /** When provided, shows a retry button; the caller re-runs its fetch. */
  onRetry?: () => void
}

/**
 * The shared error surface used when a data fetch fails.
 *
 * Shows the application error message (already mapped away from transport details by the
 * adapter) inside a red callout, with an optional retry. Always prefer this over a
 * bespoke error banner (coding-style DRY gate). Severity is carried by both the icon and
 * colour, never colour alone.
 */
export function ErrorState({ message, onRetry }: ErrorStateProps) {
  return (
    <Callout.Root color="red" mt="4" role="alert">
      <Flex direction="column" gap="2" align="start">
        <Flex gap="2" align="center">
          <Callout.Icon>
            <ExclamationTriangleIcon />
          </Callout.Icon>
          <Callout.Text>{message ?? t('error.loadFailed')}</Callout.Text>
        </Flex>
        {onRetry && (
          <Button size="1" variant="soft" color="red" onClick={onRetry}>
            <ReloadIcon />
            {t('error.tryAgain')}
          </Button>
        )}
      </Flex>
    </Callout.Root>
  )
}
