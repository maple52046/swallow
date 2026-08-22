import { Button, Flex, Heading, Text } from '@radix-ui/themes'
import { HomeIcon } from '@radix-ui/react-icons'
import { useNavigate } from 'react-router-dom'
import { t } from '@/presentation/app/i18n'

/** The catch-all 404 screen for unmatched routes. */
export function NotFoundPage() {
  const navigate = useNavigate()

  return (
    <Flex direction="column" align="center" gap="3" py="9">
      <Text as="div" size="9" weight="bold" color="gray">
        404
      </Text>
      <Heading as="h1" size="5">
        {t('notFound.title')}
      </Heading>
      <Text color="gray">{t('notFound.message')}</Text>
      <Button onClick={() => navigate('/')}>
        <HomeIcon />
        {t('notFound.backHome')}
      </Button>
    </Flex>
  )
}
