import { Box, Text } from '@chakra-ui/react'
import type { LocalRepositoryPythonRequirement } from '@/domain/localRepository/types'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { declarersText } from './localRepositoryPresentation'

/**
 * The Python requirements the modules declare for a Site's Local Repository. They have no state of
 * their own — each is fetched through the `pypi` pull-through source the first time a Server asks
 * for it — so the list shows only what is declared and by whom, and the sources table above carries
 * the sync state. The declared images have a state and are shown by `RepositoryImagesSection`.
 */
export function RepositoryDeclarationsSection({ python }: { python: readonly LocalRepositoryPythonRequirement[] }) {
  const title = 'Declared Python packages'
  return (
    <SectionSurface title={title} description="Fetched on first request through the pypi source." className="sw-local-repository-section" flush>
      {python.length === 0 ? (
        <div className="sw-section-empty">No module of this build declares a Python package.</div>
      ) : (
        <Box as="ul" className="sw-local-repository-declarations__list" aria-label={title}>
          {python.map((entry) => (
            <li key={entry.requirement}>
              <span className="sw-mono sw-local-repository__wrap">{entry.requirement}</span>
              <Text as="span" color="fg.muted" fontSize="xs">Declared by {declarersText(entry.declaredBy)}</Text>
            </li>
          ))}
        </Box>
      )}
    </SectionSurface>
  )
}
