import { Button, HStack } from '@chakra-ui/react'
import { Home, RefreshCw } from 'lucide-react'
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
      title="Page unavailable"
      message={message}
      action={
        <HStack gap="2">
          <Button colorPalette="brand" onClick={() => window.location.reload()}>
            <RefreshCw size={16} />
            Reload page
          </Button>
          <Button variant="outline" onClick={() => navigate('/')}>
            <Home size={16} />
            Open Overview
          </Button>
        </HStack>
      }
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
  return 'Reload the page or return to Overview.'
}
