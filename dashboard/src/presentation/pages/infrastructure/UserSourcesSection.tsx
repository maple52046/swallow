import { Box, Button } from '@chakra-ui/react'
import { Pencil } from 'lucide-react'
import type { LocalRepositorySpec } from '@/domain/localRepository/types'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { UserSourceSummary } from './UserSourceSummary'

interface UserSourcesSectionProps {
  spec: LocalRepositorySpec
  onManage: () => void
}

/**
 * The user's part of a Site's Local Repository (its desired state): the Repository Sources a user
 * added, such as the OS archive the installation writes, with the spec generation a write must send
 * back. The observed state of each lives in the sources table; this section shows only what was
 * asked for. "Manage user sources" opens the dialog that adds or removes them.
 */
export function UserSourcesSection({ spec, onManage }: UserSourcesSectionProps) {
  return (
    <SectionSurface
      title="User sources"
      description={`What you asked the repository to mirror besides the modules' declarations. Spec generation ${spec.generation}.`}
      actions={
        <Button size="sm" variant="outline" onClick={onManage}>
          <Pencil size={14} aria-hidden />
          Manage user sources
        </Button>
      }
      className="sw-local-repository-section"
      flush
    >
      {spec.sources.length === 0 ? (
        <div className="sw-section-empty">
          <strong>No user sources</strong>
          <span>Add an archive the modules do not declare, such as the Site&apos;s OS package archive.</span>
        </div>
      ) : (
        <Box as="ul" className="sw-local-repository-declarations__list" aria-label="User sources">
          {spec.sources.map((source) => (
            <li key={source.name}>
              <UserSourceSummary source={source} />
            </li>
          ))}
        </Box>
      )}
    </SectionSurface>
  )
}
