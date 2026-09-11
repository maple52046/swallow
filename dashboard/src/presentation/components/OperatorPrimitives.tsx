import { Flex, Heading, Text } from '@chakra-ui/react'
import type { ReactNode } from 'react'

/**
 * One operator KPI. `tone` is a semantic accent that always accompanies a visible
 * label and value, so a metric is never distinguished by colour alone.
 */
export interface StatItem {
  label: string
  value: ReactNode
  detail?: ReactNode
  tone?: 'neutral' | 'success' | 'warning' | 'critical'
}

/**
 * Compact, unframed KPI band used for cross-resource scanning.
 *
 * Structural layout comes from the shared `sw-stat-strip` styles (retuned to the
 * Chakra theme); each stat carries a data-tone the stylesheet maps to a status
 * colour for the value.
 */
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

/**
 * Consistent section heading for tables, detail groups, and workspaces. The
 * `plain` variant drops the framed chrome for headings that sit inside an already
 * bordered surface.
 */
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
        <Heading as="h2" size="md">
          {title}
        </Heading>
        {description && (
          <Text color="fg.muted" mt="1">
            {description}
          </Text>
        )}
      </div>
      {actions && <div className="sw-section-header__actions">{actions}</div>}
    </div>
  )
}

/**
 * Toolbar pinned above a large working set, optionally without outer chrome.
 *
 * A horizontal, wrapping band; consumers arrange filter/search/action controls as
 * children. The sticky positioning and surface come from the shared
 * `sw-data-toolbar` styles.
 */
export function DataToolbar({ children, variant = 'default' }: { children: ReactNode; variant?: 'default' | 'plain' }) {
  return (
    <Flex
      className={`sw-data-toolbar${variant === 'plain' ? ' sw-data-toolbar--plain' : ''}`}
      align="center"
      gap="3"
      wrap="wrap"
    >
      {children}
    </Flex>
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

/** Horizontal overflow boundary with a sticky table header for wide data tables. */
export function StickyTableFrame({ children }: { children: ReactNode }) {
  return <div className="sw-table-frame">{children}</div>
}
