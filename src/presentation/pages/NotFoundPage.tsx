import { Stack, Title, Text, Button } from '@mantine/core'
import { useNavigate } from 'react-router-dom'
import { IconHome } from '@tabler/icons-react'
import { t } from '@/presentation/app/i18n'

export function NotFoundPage() {
  const navigate = useNavigate()
  return (
    <Stack align="center" py={80} gap="md">
      <Title order={1} c="dimmed">404</Title>
      <Title order={3}>{t('notFound.title')}</Title>
      <Text c="dimmed">{t('notFound.message')}</Text>
      <Button leftSection={<IconHome size={16} />} onClick={() => navigate('/')}>
        {t('notFound.backHome')}
      </Button>
    </Stack>
  )
}
