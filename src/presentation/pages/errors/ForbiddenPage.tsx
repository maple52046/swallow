import { Button, Flex, Heading, Text } from '@radix-ui/themes'
import { ArrowLeftIcon } from '@radix-ui/react-icons'
import { useNavigate } from 'react-router-dom'

/** The 403 screen shown when a role guard blocks access to a route. */
export function ForbiddenPage() {
  const navigate = useNavigate()

  return (
    <Flex direction="column" align="center" gap="3" py="9">
      <Text as="div" size="9" weight="bold" color="gray">
        403
      </Text>
      <Heading as="h1" size="5">
        Forbidden
      </Heading>
      <Text color="gray">You do not have permission to access this page.</Text>
      <Button onClick={() => navigate('/')}>
        <ArrowLeftIcon />
        Back to Dashboard
      </Button>
    </Flex>
  )
}
