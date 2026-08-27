import {
  Button,
  EmptyState as PatternFlyEmptyState,
  EmptyStateActions,
  EmptyStateBody,
  EmptyStateFooter,
} from '@patternfly/react-core'
import { SearchIcon } from '@patternfly/react-icons'
import type { ComponentType } from 'react'

interface EmptyStateProps {
  title?: string
  message?: string
  icon?: ComponentType
  action?: { label: string; onClick: () => void }
}

/** Shared PatternFly empty result that clearly differs from loading and failure states. */
export function EmptyState({ title = 'No data', message, icon = SearchIcon, action }: EmptyStateProps) {
  return (
    <PatternFlyEmptyState headingLevel="h2" titleText={title} icon={icon}>
      {message && <EmptyStateBody>{message}</EmptyStateBody>}
      {action && (
        <EmptyStateFooter>
          <EmptyStateActions><Button onClick={action.onClick}>{action.label}</Button></EmptyStateActions>
        </EmptyStateFooter>
      )}
    </PatternFlyEmptyState>
  )
}
