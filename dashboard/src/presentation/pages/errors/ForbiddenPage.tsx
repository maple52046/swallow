import { Button } from '@patternfly/react-core'
import { ArrowLeftIcon } from '@patternfly/react-icons'
import { useNavigate } from 'react-router-dom'
import { RouteErrorTemplate } from '@/presentation/components/RouteErrorTemplate'

/** Shared 403 route shown when authorization blocks an operator surface. */
export function ForbiddenPage() {
  const navigate = useNavigate()
  return <RouteErrorTemplate code="403" title="Access denied" message="Your account does not have permission to open this operator surface." action={<Button icon={<ArrowLeftIcon />} onClick={() => navigate('/')}>Back to Overview</Button>} />
}
