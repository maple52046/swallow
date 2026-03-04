import { Group, Title, Text, type TitleOrder } from '@mantine/core'
import type { ReactNode } from 'react'

interface PageHeaderProps {
  title: string
  subtitle?: string
  order?: TitleOrder
  actions?: ReactNode
}

export function PageHeader({ title, subtitle, order = 2, actions }: PageHeaderProps) {
  return (
    <Group justify="space-between" mb="lg" align="flex-start">
      <div>
        <Title order={order}>{title}</Title>
        {subtitle && (
          <Text c="dimmed" size="sm" mt={2}>
            {subtitle}
          </Text>
        )}
      </div>
      {actions && <Group gap="xs">{actions}</Group>}
    </Group>
  )
}
