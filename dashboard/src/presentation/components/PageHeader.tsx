import {
  Breadcrumb,
  BreadcrumbItem,
  Content,
  Flex,
  FlexItem,
  Title,
} from '@patternfly/react-core'
import { Link } from 'react-router-dom'
import type { ReactNode } from 'react'

/** One breadcrumb segment; the final segment normally omits href. */
export interface PageBreadcrumb {
  label: string
  href?: string
}

interface PageHeaderProps {
  title: string
  subtitle?: string
  breadcrumbs?: PageBreadcrumb[]
  metadata?: ReactNode
  actions?: ReactNode
}

/**
 * Shared PatternFly contextual header for list and detail routes.
 * Breadcrumbs use router links, metadata stays adjacent to the title, and commands wrap
 * into a separate action row at narrow widths without overlapping the heading.
 */
export function PageHeader({ title, subtitle, breadcrumbs, metadata, actions }: PageHeaderProps) {
  return (
    <header className="sw-page-header">
      <div className="sw-page-header__main">
        {breadcrumbs && breadcrumbs.length > 0 && (
          <Breadcrumb aria-label="Breadcrumb">
            {breadcrumbs.map((item, index) => (
              <BreadcrumbItem key={`${item.label}-${index}`} isActive={!item.href}>
                {item.href ? <Link to={item.href}>{item.label}</Link> : item.label}
              </BreadcrumbItem>
            ))}
          </Breadcrumb>
        )}
        <Flex alignItems={{ default: 'alignItemsBaseline' }} gap={{ default: 'gapMd' }} flexWrap={{ default: 'wrap' }}>
          <FlexItem><Title headingLevel="h1" size="2xl">{title}</Title></FlexItem>
          {metadata && <FlexItem>{metadata}</FlexItem>}
        </Flex>
        {subtitle && <Content component="p" className="sw-page-subtitle">{subtitle}</Content>}
      </div>
      {actions && <div className="sw-page-header__actions">{actions}</div>}
    </header>
  )
}
