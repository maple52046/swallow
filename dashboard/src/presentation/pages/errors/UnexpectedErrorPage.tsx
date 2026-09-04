import { Button, Flex } from '@patternfly/react-core'
import { HomeIcon, SyncAltIcon } from '@patternfly/react-icons'
import { isRouteErrorResponse, useNavigate, useRouteError } from 'react-router-dom'
import { RouteErrorTemplate } from '@/presentation/components/RouteErrorTemplate'

/**
 * Last-resort route boundary for render and loader failures. It keeps operators inside a
 * branded recovery surface instead of exposing React Router's development exception page;
 * ordinary API failures should still use their page-level error states.
 */
export function UnexpectedErrorPage() {
  const error = useRouteError()
  const navigate = useNavigate()
  const message = routeErrorMessage(error)

  return (
    <RouteErrorTemplate
      code="500"
      title="Dashboard could not render this page"
      message={message}
      action={(
        <Flex gap={{ default: 'gapSm' }}>
          <Button icon={<SyncAltIcon />} onClick={() => window.location.reload()}>
            Reload page
          </Button>
          <Button variant="secondary" icon={<HomeIcon />} onClick={() => navigate('/')}>
            Back to Overview
          </Button>
        </Flex>
      )}
    />
  )
}

/** Maps router and render failures to operator-safe text while retaining useful local
 * diagnostics; it never serializes arbitrary thrown objects into the page. */
function routeErrorMessage(error: unknown): string {
  if (isRouteErrorResponse(error)) {
    return error.statusText || `The route failed with status ${error.status}.`
  }
  if (error instanceof Error && error.message.trim()) {
    return error.message
  }
  return 'An unexpected rendering error interrupted this page.'
}
