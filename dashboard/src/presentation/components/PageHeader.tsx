import { Box, Breadcrumb, Flex, Heading, HStack, Text } from '@chakra-ui/react'
import { Link as RouterLink } from 'react-router-dom'
import type { ReactNode } from 'react'

/** One breadcrumb segment; the final segment normally omits `href`. */
export interface PageBreadcrumb {
  label: string
  href?: string
}

interface PageHeaderProps {
  title: string
  subtitle?: string
  breadcrumbs?: PageBreadcrumb[]
  /** Inline status/metadata rendered beside the title (e.g. a StatusBadge). */
  metadata?: ReactNode
  /** Primary page commands; wrap to a second row at narrow widths. */
  actions?: ReactNode
}

/**
 * Shared contextual header for list and detail routes.
 *
 * Breadcrumbs use router links, metadata stays adjacent to the title, and commands
 * wrap onto a separate row at narrow widths without overlapping the heading. Used
 * at the top of every routed screen so page identity and primary actions stay
 * consistent.
 */
export function PageHeader({ title, subtitle, breadcrumbs, metadata, actions }: PageHeaderProps) {
  return (
    <Box as="header" pb="4" borderBottomWidth="1px" borderColor="border">
      <Flex justify="space-between" align="flex-start" gap="4" wrap="wrap">
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
          <Flex align="baseline" gap="3" wrap="wrap">
            <Heading as="h1" size="2xl" letterSpacing="tight" lineClamp={2}>
              {title}
            </Heading>
            {metadata}
          </Flex>
          {subtitle && (
            <Text color="fg.muted" maxW="78ch" mt="1.5">
              {subtitle}
            </Text>
          )}
        </Box>
        {actions && (
          <HStack gap="2" wrap="wrap">
            {actions}
          </HStack>
        )}
      </Flex>
    </Box>
  )
}
