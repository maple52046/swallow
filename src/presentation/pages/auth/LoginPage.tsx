import { useEffect, useState } from 'react'
import { Alert, Button, Card, Container, Group, PasswordInput, Stack, Text, TextInput, Title } from '@mantine/core'
import { IconAlertCircle, IconLogin2 } from '@tabler/icons-react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '@/presentation/contexts/AuthContext'

interface LoginLocationState {
  from?: string
}

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
      // Deep-linking to a protected page arrives here already authenticated, because
      // the route guard redirects before the session check resolves. Sending everyone
      // to the default route would silently discard the page they asked for.
      navigate(returnPath, { replace: true })
    }
  }, [isAuthenticated, navigate, returnPath])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!username || !password || submitting) return
    try {
      setSubmitting(true)
      setError(null)
      await login(username.trim(), password)
      navigate(returnPath, { replace: true })
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Invalid username or password')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Container size={460} py={80}>
      <Card withBorder radius="md" p="xl">
        <form onSubmit={(e) => void handleSubmit(e)}>
          <Stack gap="md">
            <div>
              <Title order={2}>Sign in</Title>
              <Text c="dimmed" size="sm">Datacenter Prototype Auth Demo</Text>
              {currentUser && (
                <Text c="dimmed" size="xs" mt={4}>
                  Current: {currentUser.displayName} ({currentUser.role})
                </Text>
              )}
            </div>

            {error && (
              <Alert color="red" icon={<IconAlertCircle size={16} />} title="Login failed">
                {error}
              </Alert>
            )}

            <TextInput
              label="Username"
              value={username}
              onChange={(e) => setUsername(e.currentTarget.value)}
              placeholder="Enter your account identifier"
              autoComplete="username"
              required
            />

            <PasswordInput
              label="Password"
              value={password}
              onChange={(e) => setPassword(e.currentTarget.value)}
              placeholder="Enter your password"
              autoComplete="current-password"
              required
            />

            <Group justify="flex-end">
              <Button
                type="submit"
                leftSection={<IconLogin2 size={16} />}
                loading={submitting}
                disabled={!username || !password}
              >
                Login
              </Button>
            </Group>
          </Stack>
        </form>
      </Card>
    </Container>
  )
}
