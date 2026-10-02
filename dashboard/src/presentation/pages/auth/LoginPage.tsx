import { useEffect, useState } from 'react'
import { Box, Button, Card, chakra, Field, Flex, Heading, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { useLocation, useNavigate } from 'react-router-dom'
import { SwallowLogo } from '@/presentation/components/SwallowLogo'
import { Alert } from '@/presentation/components/ui/alert'
import { useAuth } from '@/presentation/contexts/AuthContext'

interface LoginLocationState {
  from?: string
}

/**
 * Sign-in route backed by the real authentication API.
 *
 * When the Session ended on its own (expired or revoked) the form explains why the operator is
 * here; after sign-in they return to the page they were on.
 *
 * Only same-origin return paths are honoured. Authentication failures remain
 * anchored to the form, fields preserve native autocomplete, and the submit
 * action exposes its in-flight state without hiding the entered credentials.
 */
export function LoginPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const { login, isAuthenticated, sessionEnded } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const state = location.state as LoginLocationState | null
  const from = state?.from
  const returnPath = from && from.startsWith('/') && !from.startsWith('//') ? from : '/'

  // Auth can change outside this form. Redirect only to the prevalidated path
  // whenever the shared session boundary reports a signed-in user.
  useEffect(() => {
    if (isAuthenticated) navigate(returnPath, { replace: true })
  }, [isAuthenticated, navigate, returnPath])

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!username.trim() || !password || submitting) return
    setSubmitting(true)
    setError(null)
    try {
      await login(username.trim(), password)
      navigate(returnPath, { replace: true })
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Sign in failed')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Flex data-testid="login-shell" minH="100dvh" align="center" justify="center" bg="transparent" p={{ base: '4', md: '8' }}>
      <Flex
        data-testid="login-panel"
        w="full"
        maxW="58rem"
        minH={{ md: '34rem' }}
        overflow="hidden"
        rounded="2xl"
        borderWidth="1px"
        borderColor="border.muted"
        bg="bg.panel"
        boxShadow="floating"
      >
        <Flex
          display={{ base: 'none', md: 'flex' }}
          position="relative"
          flex="0 0 42%"
          direction="column"
          justify="space-between"
          overflow="hidden"
          p="10"
          bg="brand.solid"
          color="white"
        >
          <Box
            position="absolute"
            insetInlineEnd="-24"
            top="-20"
            w="64"
            h="64"
            rounded="full"
            borderWidth="32px"
            borderColor="whiteAlpha.200"
            aria-hidden
          />
          <HStack position="relative" gap="3">
            <Box p="2" rounded="xl" bg="whiteAlpha.200"><SwallowLogo /></Box>
            <Text fontSize="xl" fontWeight="bold" letterSpacing="tight">Swallow</Text>
          </HStack>
          <Box position="relative">
            <Heading size="3xl" maxW="12ch" fontWeight="semibold" letterSpacing="tight">
              Infrastructure, clearly managed.
            </Heading>
            <Text mt="4" color="whiteAlpha.800">Operate servers, platforms, and workflows.</Text>
          </Box>
        </Flex>

        <Card.Root flex="1" border="0" boxShadow="none" rounded="0" bg="bg.panel">
          <Card.Body justifyContent="center" gap="7" p={{ base: '6', md: '10' }}>
            <Stack gap="3">
              <HStack display={{ base: 'flex', md: 'none' }} gap="2" color="brand.solid">
                <SwallowLogo />
                <Text fontSize="xl" fontWeight="bold" color="fg" letterSpacing="tight">Swallow</Text>
              </HStack>
              <Heading size="2xl" fontWeight="semibold">Sign in</Heading>
              <Text color="fg.muted">Continue to the operator console.</Text>
            </Stack>
            {error && <Alert status="error" title={error} />}
            {!error && sessionEnded && (
              <Alert status="info" title="Your session ended">Sign in again to continue where you left off.</Alert>
            )}
            <chakra.form onSubmit={(event) => void submit(event)}>
              <Stack gap="5">
                <Field.Root required>
                  <Field.Label htmlFor="sw-login-username">Username <Field.RequiredIndicator /></Field.Label>
                  <Input
                    id="sw-login-username"
                    value={username}
                    onChange={(event) => setUsername(event.target.value)}
                    autoComplete="username"
                    size="lg"
                  />
                </Field.Root>
                <Field.Root required>
                  <Field.Label htmlFor="sw-login-password">Password <Field.RequiredIndicator /></Field.Label>
                  <Input
                    id="sw-login-password"
                    type="password"
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                    autoComplete="current-password"
                    size="lg"
                  />
                </Field.Root>
                <Button
                  type="submit"
                  colorPalette="brand"
                  width="full"
                  size="lg"
                  loading={submitting}
                  disabled={!username.trim() || !password || submitting}
                >
                  Sign in
                </Button>
              </Stack>
            </chakra.form>
          </Card.Body>
        </Card.Root>
      </Flex>
    </Flex>
  )
}
