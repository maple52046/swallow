import { Badge, Box, Text } from '@chakra-ui/react'
import type { RepositorySourceSpec } from '@/domain/localRepository/types'
import { sourceScopeText } from './localRepositoryPresentation'

/**
 * One Repository Source of the user's spec as a list row: its name and format, its upstream URL,
 * and the suites, components, and architectures it takes. Shared by the User sources section and
 * the Manage user sources dialog so a source reads the same where it is shown and where it is
 * removed. Long URLs wrap rather than widen a narrow viewport.
 */
export function UserSourceSummary({ source }: { source: RepositorySourceSpec }) {
  const scope = sourceScopeText(source)
  return (
    <Box minW="0">
      <Box display="flex" alignItems="center" gap="2" flexWrap="wrap">
        <strong>{source.name}</strong>
        <Badge variant="outline" className="sw-mono">{source.format}</Badge>
      </Box>
      <Text fontSize="sm" color="fg.muted" className="sw-mono sw-local-repository__wrap">{source.url}</Text>
      {scope && <Text fontSize="xs" color="fg.muted">{scope}</Text>}
    </Box>
  )
}
