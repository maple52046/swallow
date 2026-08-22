import { useEffect, useState } from 'react'
import { Box, Button, Callout, Card, Flex, Heading, Text, TextField } from '@radix-ui/themes'
import { EnterIcon, ExclamationTriangleIcon } from '@radix-ui/react-icons'
import { useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '@/presentation/contexts/AuthContext'

interface LoginLocationState {
  from?: string
}

/**
 * The unauthenticated sign-in screen.
 *
 * On success it returns the user to the page they were sent from (`location.state.from`,
 * set by `ProtectedRoute`), so a deep link survives login. Only same-origin paths are
 * honoured, so a crafted `from` cannot bounce someone off-site. An already-authenticated
 * visitor is redirected away by the effect rather than shown the form.
 */
export function LoginPage() {
  const navigate = useNavigate()
  const location = useLocation()
  const { login, isAuthenticated, currentUser } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Only same-origin paths are honoured, so that a crafted `from` cannot be used to
  // bounce someone off-site after a successful login.
  const returnPath = (() => {
    const state = location.state as LoginLocationState | null
    const from = state?.from
    return from && from.startsWith('/') && !from.startsWith('//') ? from : '/'
  })()

  useEffect(() => {
    if (isAuthenticated) {
      // Deep-linking to a protected page arrives here already authenticated, because the
      // route guard redirects before the session check resolves. Sending everyone to the
      // default route would silently discard the page they asked for.
      navigate(returnPath, { replace: true })
    }
  }, [isAuthenticated, navigate, returnPath])

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (!username || !password || submitting) return
    try {
      setSubmitting(true)
      setError(null)
      await login(username.trim(), password)
      navigate(returnPath, { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Invalid username or password')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Flex justify="center" px="4" py="9">
      <Box style={{ width: '100%', maxWidth: 420 }}>
        <Card size="4">
          <form onSubmit={(event) => void handleSubmit(event)}>
            <Flex direction="column" gap="4">
              <Flex direction="column" gap="1">
                <Heading as="h1" size="6">
                  Sign in
                </Heading>
                <Text color="gray" size="2">
                  Datacenter Prototype Auth Demo
                </Text>
                {currentUser && (
                  <Text color="gray" size="1">
                    Current: {currentUser.displayName} ({currentUser.role})
                  </Text>
                )}
              </Flex>

              {error && (
                <Callout.Root color="red" role="alert">
                  <Callout.Icon>
                    <ExclamationTriangleIcon />
                  </Callout.Icon>
                  <Callout.Text>{error}</Callout.Text>
                </Callout.Root>
              )}

              <label>
                <Text as="div" size="2" weight="medium" mb="1">
                  Username
                </Text>
                <TextField.Root
                  value={username}
                  onChange={(event) => setUsername(event.currentTarget.value)}
                  placeholder="Enter your account identifier"
                  autoComplete="username"
                  required
                />
              </label>

              <label>
                <Text as="div" size="2" weight="medium" mb="1">
                  Password
                </Text>
                <TextField.Root
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.currentTarget.value)}
                  placeholder="Enter your password"
                  autoComplete="current-password"
                  required
                />
              </label>

              <Flex justify="end">
                <Button type="submit" loading={submitting} disabled={!username || !password}>
                  <EnterIcon />
                  Login
                </Button>
              </Flex>
            </Flex>
          </form>
        </Card>
      </Box>
    </Flex>
  )
}
