import { useEffect, useState } from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  Card,
  CardBody,
  Content,
  Form,
  FormGroup,
  LoginMainBody,
  LoginMainHeader,
  LoginPage as PatternFlyLoginPage,
  TextInput,
  Title,
} from '@patternfly/react-core'
import { useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '@/presentation/contexts/AuthContext'

interface LoginLocationState { from?: string }

/**
 * PatternFly sign-in route backed by the real authentication API.
 * Only same-origin return paths are honored, preventing crafted navigation state from
 * turning a successful login into an external redirect.
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
    <PatternFlyLoginPage className="sw-login" loginTitle="Swallow" loginSubtitle="Operator console">
      <LoginMainHeader>
        <div className="sw-login-brand"><span className="sw-brand-mark" aria-hidden="true">S</span><Title headingLevel="h1">Swallow</Title></div>
        <Content component="p">Sign in to manage infrastructure, clusters, and automation.</Content>
      </LoginMainHeader>
      <LoginMainBody>
        <Card isPlain><CardBody>
          {error && <Alert variant={AlertVariant.danger} title={error} isInline className="sw-login-error" />}
          <Form onSubmit={(event) => void submit(event)}>
            <FormGroup label="Username" isRequired fieldId="sw-login-username"><TextInput id="sw-login-username" value={username} onChange={(_event, value) => setUsername(value)} autoComplete="username" isRequired /></FormGroup>
            <FormGroup label="Password" isRequired fieldId="sw-login-password"><TextInput id="sw-login-password" type="password" value={password} onChange={(_event, value) => setPassword(value)} autoComplete="current-password" isRequired /></FormGroup>
            <Button type="submit" isBlock isLoading={submitting} isDisabled={!username.trim() || !password || submitting}>Sign in</Button>
          </Form>
        </CardBody></Card>
      </LoginMainBody>
    </PatternFlyLoginPage>
  )
}
