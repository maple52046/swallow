import { Box, Breadcrumb, Flex, Heading, HStack, Text } from '@chakra-ui/react'
import type { ReactNode } from 'react'
import { Link as RouterLink } from 'react-router-dom'

/** One breadcrumb segment; the final segment normally omits `href`. */
export interface PageBreadcrumb {
  label: string
  href?: string
}

interface PageHeaderProps {
  title: string
  subtitle?: string
  breadcrumbs?: PageBreadcrumb[]
  /** Compact status or freshness metadata displayed beside the route title. */
  metadata?: ReactNode
  /** Primary route commands; they wrap below the heading on narrow screens. */
  actions?: ReactNode
  /** Gives action-heavy detail pages a dedicated mobile action row. */
  stackActionsOnMobile?: boolean
}

/**
 * Shared route heading for list, detail, and workflow pages.
 *
 * The component deliberately has no enclosing divider: page identity is separated
 * through spacing and type hierarchy, leaving bordered surfaces for actionable
 * content. Breadcrumbs stay native router links and actions keep source order.
 */
export function PageHeader({ title, subtitle, breadcrumbs, metadata, actions, stackActionsOnMobile = false }: PageHeaderProps) {
  return (
    <Box as="header" className="sw-page-header">
      <Flex
        justify="space-between"
        align={{ base: 'flex-start', md: 'center' }}
        direction={stackActionsOnMobile ? { base: 'column', md: 'row' } : 'row'}
        gap="4"
        wrap="wrap"
      >
        <Box minW="0" flex="1">
          {breadcrumbs && breadcrumbs.length > 0 && (
            <Breadcrumb.Root mb="1.5" fontSize="sm">
              <Breadcrumb.List>
                {breadcrumbs.map((item, index) => (
                  <Box as="span" display="contents" key={`${item.label}-${index}`}>
                    <Breadcrumb.Item>
                      {item.href ? (
                        <Breadcrumb.Link asChild>
                          <RouterLink to={item.href}>{item.label}</RouterLink>
                        </Breadcrumb.Link>
                      ) : (
                        <Breadcrumb.CurrentLink>{item.label}</Breadcrumb.CurrentLink>
                      )}
                    </Breadcrumb.Item>
                    {index < breadcrumbs.length - 1 && <Breadcrumb.Separator />}
                  </Box>
                ))}
              </Breadcrumb.List>
            </Breadcrumb.Root>
          )}
          <Flex align="center" gap="3" wrap="wrap">
            <Heading as="h1" size="3xl" fontWeight="semibold" letterSpacing="tight" lineClamp={2}>
              {title}
            </Heading>
            {metadata}
          </Flex>
          {subtitle && (
            <Text color="fg.muted" maxW="68ch" mt="1">
              {subtitle}
            </Text>
          )}
        </Box>
        {actions && (
          <HStack gap="2" wrap="wrap" width={stackActionsOnMobile ? { base: '100%', md: 'auto' } : undefined}>
            {actions}
          </HStack>
        )}
      </Flex>
    </Box>
  )
}
