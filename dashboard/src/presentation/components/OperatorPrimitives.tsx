import { Box, Button, Flex, Heading, HStack, Portal, Text } from '@chakra-ui/react'
import { X } from 'lucide-react'
import type { ReactNode } from 'react'

/** One current-state metric. Tone supplements the visible label and value. */
export interface MetricItem {
  label: string
  value: ReactNode
  detail?: ReactNode
  tone?: 'neutral' | 'success' | 'warning' | 'critical'
  /** Optional decorative glyph that helps distinguish peer metrics when scanning. */
  icon?: ReactNode
}

/**
 * Responsive metric-card grid for current operational facts.
 *
 * Cards provide hierarchy without implying historical trends. Tone is reflected
 * in an accent and the value but meaning always remains in visible text.
 */
export function MetricGrid({ items }: { items: readonly MetricItem[] }) {
  return (
    <Box as="dl" className="sw-metric-grid">
      {items.map((item) => (
        <Box as="div" key={item.label} className="sw-metric-card" data-tone={item.tone ?? 'neutral'}>
          <HStack as="dt" justify="space-between" gap="3">
            <span>{item.label}</span>
            {item.icon && <Box className="sw-metric-card__icon" aria-hidden>{item.icon}</Box>}
          </HStack>
          <Box as="dd">{item.value}</Box>
          {item.detail && <Text as="span">{item.detail}</Text>}
        </Box>
      ))}
    </Box>
  )
}

/**
 * Consistent section heading for tables, detail groups, and workspaces.
 * The plain variant sits above an already framed surface.
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
        <Heading as="h2" size="md" fontWeight="semibold">
          {title}
        </Heading>
        {description && <Text color="fg.muted" mt="1">{description}</Text>}
      </div>
      {actions && <div className="sw-section-header__actions">{actions}</div>}
    </div>
  )
}

interface SectionSurfaceProps {
  title?: string
  description?: string
  actions?: ReactNode
  children: ReactNode
  /** Omits body padding for tables and other edge-to-edge workspaces. */
  flush?: boolean
  className?: string
}

/**
 * Shared elevated content group used across route pages.
 *
 * Heading and body chrome are composed once so list, detail, and monitoring pages
 * cannot drift into slightly different card structures. `flush` is reserved for
 * content that owns its own row or table spacing.
 */
export function SectionSurface({ title, description, actions, children, flush = false, className }: SectionSurfaceProps) {
  const classes = ['sw-section', className].filter(Boolean).join(' ')
  return (
    <section className={classes}>
      {title && <SectionHeader title={title} description={description} actions={actions} />}
      <div className={flush ? 'sw-section-content sw-section-content--flush' : 'sw-section-content'}>{children}</div>
    </section>
  )
}

/** Shared inventory frame for resource lists without prescribing their row content. */
interface InventorySurfaceProps {
  /** Stable heading id used by the section's accessible name. */
  headingId: string
  /** Small domain label above the inventory title. */
  eyebrow: string
  title: string
  /** Live result or refresh metadata announced as one atomic status. */
  summary: ReactNode
  /** Optional URL-owned discovery controls rendered above the working set. */
  toolbar?: ReactNode
  children: ReactNode
  /** Feature modifier for layout differences that do not belong in the shared chrome. */
  className?: string
}

/**
 * Shared control-plane inventory chrome for resource and execution lists.
 *
 * The heading, result status, filter band, border, and elevation are composed
 * once so Platform and Workflow inventories retain the same visual hierarchy.
 * Consumers continue to own their filters, loading states, responsive rows,
 * and domain-specific actions.
 */
export function InventorySurface({
  headingId,
  eyebrow,
  title,
  summary,
  toolbar,
  children,
  className,
}: InventorySurfaceProps) {
  const classes = ['sw-inventory-surface', className].filter(Boolean).join(' ')
  return (
    <Box as="section" className={classes} aria-labelledby={headingId}>
      <div className="sw-inventory-surface__heading">
        <div>
          <Text className="sw-inventory-surface__eyebrow">{eyebrow}</Text>
          <Heading as="h2" id={headingId} size="lg">{title}</Heading>
        </div>
        <Box
          className="sw-inventory-surface__summary"
          color="fg.muted"
          role="status"
          aria-live="polite"
          aria-atomic="true"
        >
          {summary}
        </Box>
      </div>
      {toolbar && <div className="sw-inventory-surface__toolbar">{toolbar}</div>}
      {children}
    </Box>
  )
}

/**
 * Toolbar pinned above a large working set.
 * Consumers own filtering state while this component standardizes wrapping,
 * surface, and touch-target spacing.
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

interface SelectionToolbarProps {
  count: number
  onClear: () => void
  children: ReactNode
}

/**
 * Shared floating bulk-action dock for list pages.
 *
 * The dock is portalled outside each page toolbar so appearing selection actions
 * never reflow the table beneath them. The count remains a live text status, and
 * the clear action stays last so keyboard order matches the visual workflow.
 */
export function SelectionToolbar({ count, onClear, children }: SelectionToolbarProps) {
  if (count === 0) return null
  return (
    <Portal>
      <HStack className="sw-selection-toolbar" gap="2" role="region" aria-label="Selection actions">
        <HStack className="sw-selection-toolbar__status" gap="2" role="status" aria-atomic="true">
          <Box as="span" className="sw-selection-toolbar__count">{count}</Box>
          {' '}
          <Text as="span" fontWeight="semibold">selected</Text>
        </HStack>
        <span className="sw-selection-toolbar__divider" aria-hidden />
        <HStack className="sw-selection-toolbar__actions" gap="2">
          {children}
        </HStack>
        <Button className="sw-selection-toolbar__clear" variant="plain" size="sm" onClick={onClear}>
          <X size={16} />
          Clear
        </Button>
      </HStack>
    </Portal>
  )
}

/** Dense definition-list grid for machine and workflow metadata. */
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

/** Compact identity facts placed directly below a detail-page heading. */
export function IdentitySummary({ items }: { items: Array<{ label: string; value: ReactNode }> }) {
  return (
    <Box className="sw-identity-summary">
      <KeyValueGrid items={items} />
    </Box>
  )
}

/** Horizontal overflow boundary with a sticky table header for wide desktop data. */
export function StickyTableFrame({ children }: { children: ReactNode }) {
  return <div className="sw-table-frame">{children}</div>
}
