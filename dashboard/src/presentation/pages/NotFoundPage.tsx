import { Button } from '@patternfly/react-core'
import { HomeIcon } from '@patternfly/react-icons'
import { useNavigate } from 'react-router-dom'
import { RouteErrorTemplate } from '@/presentation/components/RouteErrorTemplate'

/** Catch-all 404 state that returns authenticated operators to Overview. */
export function NotFoundPage() {
  const navigate = useNavigate()
  return <RouteErrorTemplate code="404" title="Page not found" message="The requested Swallow route does not exist or is no longer available." action={<Button icon={<HomeIcon />} onClick={() => navigate('/')}>Back to Overview</Button>} />
}
