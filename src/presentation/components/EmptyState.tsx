import { Stack, Text, Button, ThemeIcon, type StackProps } from '@mantine/core'
import { IconInbox } from '@tabler/icons-react'
import type { ReactNode } from 'react'

interface EmptyStateProps extends StackProps {
  title?: string
  message?: string
  icon?: ReactNode
  action?: { label: string; onClick: () => void }
}

export function EmptyState({ title = 'No data', message, icon, action, ...rest }: EmptyStateProps) {
  return (
    <Stack align="center" py="xl" gap="sm" {...rest}>
      <ThemeIcon variant="light" size="xl" radius="xl" color="gray">
        {icon ?? <IconInbox size={24} />}
      </ThemeIcon>
      <Text fw={500} c="dimmed">
        {title}
      </Text>
      {message && (
        <Text size="sm" c="dimmed" ta="center" maw={360}>
          {message}
        </Text>
      )}
      {action && (
        <Button variant="light" size="sm" onClick={action.onClick}>
          {action.label}
        </Button>
      )}
    </Stack>
  )
}
