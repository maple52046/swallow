import { Box, Grid, HStack, Text } from '@chakra-ui/react'
import type { ReactNode } from 'react'

interface ResponsiveDataViewProps {
  /** Semantic table rendered at the `md` breakpoint and above. */
  desktop: ReactNode
  /** Equivalent card collection rendered below the `md` breakpoint. */
  mobile: ReactNode
}

/**
 * Switches a data collection between a dense desktop table and mobile cards.
 *
 * Both renderers consume the same already-filtered items and callbacks. CSS
 * display removes the inactive representation from the accessibility tree, so
 * controls and row content are not announced twice.
 */
export function ResponsiveDataView({ desktop, mobile }: ResponsiveDataViewProps) {
  return (
    <>
      <Box display={{ base: 'none', md: 'block' }}>{desktop}</Box>
      <Box display={{ base: 'block', md: 'none' }}>{mobile}</Box>
    </>
  )
}

interface ResourceCardProps {
  title: ReactNode
  description?: ReactNode
  status?: ReactNode
  actions?: ReactNode
  details?: ReactNode
  children: ReactNode
  selected?: boolean
}

/**
 * Mobile representation of one resource-row.
 *
 * The title and status remain first in reading order, fields follow as a compact
 * definition grid, and actions stay visible rather than depending on hover.
 */
export function ResourceCard({ title, description, status, actions, details, children, selected = false }: ResourceCardProps) {
  return (
    <Box as="article" className="sw-resource-card" data-selected={selected || undefined}>
      <HStack align="flex-start" justify="space-between" gap="3">
        <Box minW="0">
          <Box fontWeight="semibold">{title}</Box>
          {description && <Text color="fg.muted" fontSize="sm" mt="0.5" lineClamp={2}>{description}</Text>}
        </Box>
        {status}
      </HStack>
      <Grid as="dl" className="sw-resource-card__fields">{children}</Grid>
      {details && (
        <Box as="details" className="sw-resource-card__details">
          <Box as="summary">More details</Box>
          <Grid as="dl" className="sw-resource-card__fields">{details}</Grid>
        </Box>
      )}
      {actions && <HStack className="sw-resource-card__actions" gap="2" wrap="wrap">{actions}</HStack>}
    </Box>
  )
}

/** One label/value fact within a mobile resource card. */
export function ResourceCardField({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Box as="div">
      <Text as="dt">{label}</Text>
      <Box as="dd">{children}</Box>
    </Box>
  )
}
