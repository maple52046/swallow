import { Stack, Text, Button, Alert } from '@mantine/core'
import { IconAlertCircle, IconRefresh } from '@tabler/icons-react'
import { t } from '@/presentation/app/i18n'

interface ErrorStateProps {
  message?: string
  onRetry?: () => void
}

export function ErrorState({ message, onRetry }: ErrorStateProps) {
  return (
    <Alert icon={<IconAlertCircle size={16} />} color="red" variant="light" mt="md">
      <Stack gap="xs">
        <Text size="sm">{message ?? t('error.loadFailed')}</Text>
        {onRetry && (
          <Button size="xs" variant="light" color="red" leftSection={<IconRefresh size={14} />} onClick={onRetry} w="fit-content">
            {t('error.tryAgain')}
          </Button>
        )}
      </Stack>
    </Alert>
  )
}
