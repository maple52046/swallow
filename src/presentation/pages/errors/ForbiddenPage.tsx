import { Button, Stack, Text, Title } from '@mantine/core'
import { IconArrowLeft } from '@tabler/icons-react'
import { useNavigate } from 'react-router-dom'

export function ForbiddenPage() {
  const navigate = useNavigate()

  return (
    <Stack align="center" py={80} gap="md">
      <Title order={1} c="dimmed">403</Title>
      <Title order={3}>Forbidden</Title>
      <Text c="dimmed">You do not have permission to access this page.</Text>
      <Button leftSection={<IconArrowLeft size={16} />} onClick={() => navigate('/')}>
        Back to Dashboard
      </Button>
    </Stack>
  )
}
