import { Content, Title, Toolbar, ToolbarContent } from '@patternfly/react-core'
import type { ReactNode } from 'react'

/** One operator KPI. Tone is semantic and always accompanies a visible label/detail. */
export interface StatItem {
  label: string
  value: ReactNode
  detail?: ReactNode
  tone?: 'neutral' | 'success' | 'warning' | 'critical'
}

/** Compact unframed KPI band used for cross-resource scanning. */
export function StatStrip({ items }: { items: StatItem[] }) {
  return (
    <dl className="sw-stat-strip">
      {items.map((item) => (
        <div key={item.label} className="sw-stat" data-tone={item.tone ?? 'neutral'}>
          <dt>{item.label}</dt>
          <dd>{item.value}</dd>
          {item.detail && <span>{item.detail}</span>}
        </div>
      ))}
    </dl>
  )
}

/** Consistent section heading for tables, detail groups, and workspaces. */
export function SectionHeader({
  title,
  description,
  actions,
  variant = 'contained',
}: {
  title: string
  description?: string
  actions?: ReactNode
  variant?: 'contained' | 'plain'
}) {
  return (
    <div className={`sw-section-header${variant === 'plain' ? ' sw-section-header--plain' : ''}`}>
      <div>
        <Title headingLevel="h2" size="lg">{title}</Title>
        {description && <Content component="p">{description}</Content>}
      </div>
      {actions && <div className="sw-section-header__actions">{actions}</div>}
    </div>
  )
}

/** PatternFly toolbar pinned above a large working set, optionally without outer chrome. */
export function DataToolbar({ children, variant = 'default' }: { children: ReactNode; variant?: 'default' | 'plain' }) {
  return (
    <Toolbar className={`sw-data-toolbar${variant === 'plain' ? ' sw-data-toolbar--plain' : ''}`} clearAllFilters={() => undefined}>
      <ToolbarContent>{children}</ToolbarContent>
    </Toolbar>
  )
}

/** Dense definition-list grid for machine and operation metadata. */
export function KeyValueGrid({ items }: { items: Array<{ label: string; value: ReactNode }> }) {
  return (
    <dl className="sw-key-value-grid">
      {items.map((item) => (
        <div key={item.label}>
          <dt>{item.label}</dt>
          <dd>{item.value}</dd>
        </div>
      ))}
    </dl>
  )
}

/** Horizontal overflow boundary with a sticky PatternFly table header. */
export function StickyTableFrame({ children }: { children: ReactNode }) {
  return <div className="sw-table-frame">{children}</div>
}
