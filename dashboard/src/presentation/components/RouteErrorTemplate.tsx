import { EmptyState, EmptyStateBody, EmptyStateFooter } from '@patternfly/react-core'
import type { ReactNode } from 'react'

import { SwallowLogo } from './SwallowLogo'
/** Branded PatternFly full-page template shared by 403 and 404 routes. */
export function RouteErrorTemplate({ code, title, message, action }: { code: string; title: string; message: string; action: ReactNode }) {
  return (
    <main className="sw-route-error">
      <div className="sw-route-error__brand"><SwallowLogo /><strong>Swallow</strong></div>
      <EmptyState headingLevel="h1" titleText={title}>
        <span className="sw-route-error__code" aria-hidden="true">{code}</span>
        <EmptyStateBody>{message}</EmptyStateBody>
        <EmptyStateFooter>{action}</EmptyStateFooter>
      </EmptyState>
    </main>
  )
}
