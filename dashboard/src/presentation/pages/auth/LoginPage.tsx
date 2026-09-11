import { useEffect, useState } from 'react'
import { Button, Card, chakra, Field, Flex, Heading, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { SwallowLogo } from '@/presentation/components/SwallowLogo'
import { Alert } from '@/presentation/components/ui/alert'

interface LoginLocationState {
  from?: string
}

/**
 * Sign-in route backed by the real authentication API.
 *
 * Only same-origin return paths are honoured, preventing crafted navigation state
 * from turning a successful login into an external redirect. The submit button
 * stays disabled until both fields are non-empty and reflects the in-flight
 * request, and auth failures surface as an inline alert rather than a toast so the
 * message stays anchored to the form.
 */
export function LoginPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const { login, isAuthenticated } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const state = location.state as LoginLocationState | null
  const from = state?.from
  const returnPath = from && from.startsWith('/') && !from.startsWith('//') ? from : '/'

  // Auth state can change outside this form. Redirect only to the prevalidated
  // same-origin path whenever the context reports an authenticated session.
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
      setError(caught instanceof Error ? caught.message : 'Invalid username or password')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Flex minH="100dvh" align="center" justify="center" bg="bg.subtle" p="4">
      <Card.Root w="full" maxW="26rem">
        <Card.Body gap="6">
          <Stack gap="2">
            <HStack gap="2" color="brand.solid">
              <SwallowLogo />
              <Text fontSize="2xl" fontWeight="bold" color="fg" letterSpacing="tight">
                Swallow
              </Text>
            </HStack>
            <Heading size="lg">Sign in</Heading>
            <Text color="fg.muted">Operator console — manage infrastructure, platforms, and automation.</Text>
          </Stack>
          {error && <Alert status="error" title={error} />}
          <chakra.form onSubmit={(event) => void submit(event)}>
            <Stack gap="4">
              <Field.Root required>
                <Field.Label>
                  Username <Field.RequiredIndicator />
                </Field.Label>
                <Input
                  id="sw-login-username"
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  autoComplete="username"
                />
              </Field.Root>
              <Field.Root required>
                <Field.Label>
                  Password <Field.RequiredIndicator />
                </Field.Label>
                <Input
                  id="sw-login-password"
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  autoComplete="current-password"
                />
              </Field.Root>
              <Button
                type="submit"
                colorPalette="brand"
                width="full"
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
  )
}
