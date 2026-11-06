import { useState, type KeyboardEvent } from 'react'
import { Box, Button, Field, Heading, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { Lock, Plus, Trash2 } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import {
  isUserDeclared,
  LocalRepositorySpecConflictError,
  type LocalRepository,
  type RepositorySourceSpec,
} from '@/domain/localRepository/types'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { declarersText } from './localRepositoryPresentation'
import { UserSourceSummary } from './UserSourceSummary'
import {
  debSourceFromDraft,
  debSourceProblems,
  EMPTY_DEB_SOURCE_DRAFT,
  type DebSourceDraft,
} from './userSourceForm'

interface UserSourcesDialogProps {
  siteId: string
  /** The repository as the page last read it; a re-read after a conflict arrives through this prop. */
  repository: LocalRepository
  onClose: () => void
  /** Receives the stored Local Repository after a successful write. */
  onSaved: (repository: LocalRepository) => void
  /** Asks the page to read the repository again after the API refused a stale generation. */
  onConflict: () => void
}

/**
 * The working copy of the user's sources: the spec generation and list it was based on, and the
 * list as edited. Saving sends `sources` with `generation`.
 */
interface SpecDraft {
  generation: number
  base: readonly RepositorySourceSpec[]
  sources: readonly RepositorySourceSpec[]
}

function draftOf(repository: LocalRepository): SpecDraft {
  return { generation: repository.spec.generation, base: repository.spec.sources, sources: repository.spec.sources }
}

const ADD_FIELDS: ReadonlyArray<{ field: keyof DebSourceDraft; label: string; placeholder: string; help?: string; required: boolean }> = [
  { field: 'name', label: 'Name', placeholder: 'ubuntu-ports', help: 'Part of the address Servers read the source at.', required: true },
  { field: 'url', label: 'URL', placeholder: 'http://archive.ubuntu.com/ubuntu', required: true },
  { field: 'suites', label: 'Suites', placeholder: 'noble, noble-updates', help: 'Separate words with commas or spaces.', required: true },
  { field: 'components', label: 'Components', placeholder: 'main, universe', required: true },
  { field: 'architectures', label: 'Architectures', placeholder: 'amd64', help: 'Optional.', required: false },
]

/**
 * Manage user sources: the one place the dashboard changes a Site's Local Repository spec. A user
 * removes any of their sources and adds Debian/Ubuntu (`deb`) sources — name, URL, suites,
 * components, and optional architectures — to a working copy, then saves the whole list in one
 * `PUT .../spec` with the generation it was based on. Sources only modules declare (including the
 * derived pull-through sources) are listed read-only with a lock, because they change with the
 * module, not here.
 *
 * - Each added source is checked with {@link debSourceProblems} first; the API's own refusal (for
 *   example a contradiction it finds) shows in the dialog with its request ID.
 * - A `409` means the sources changed since they were read: nothing was written, the page reads the
 *   repository again, and once the newer generation arrives the working copy restarts from it with a
 *   warning, so another admin's change is never overwritten silently. The Add form keeps its fields.
 * - Enter in an Add field adds the source rather than saving, since the two are separate steps.
 * - Dismissal is blocked while saving.
 */
export function UserSourcesDialog({ siteId, repository, onClose, onSaved, onConflict }: UserSourcesDialogProps) {
  const { localRepository } = useApp()
  const [draft, setDraft] = useState<SpecDraft>(() => draftOf(repository))
  const [addDraft, setAddDraft] = useState<DebSourceDraft>(EMPTY_DEB_SOURCE_DRAFT)
  const [addAttempted, setAddAttempted] = useState(false)
  const [conflict, setConflict] = useState<'waiting' | 'rebased' | null>(null)
  const [error, setError] = useState<DisplayedFailure | null>(null)
  const [saving, setSaving] = useState(false)

  // Adjusting state during render is React's pattern for reacting to a changed prop: once the
  // re-read after a conflict brings a newer generation, the working copy restarts from it.
  if (conflict === 'waiting' && repository.spec.generation !== draft.generation) {
    setDraft(draftOf(repository))
    setConflict('rebased')
  }

  const moduleSources = repository.sources.filter((source) => !isUserDeclared(source))
  const problems = debSourceProblems(addDraft, { userSources: draft.sources, effectiveSources: repository.sources })
  const addValid = Object.keys(problems).length === 0
  const dirty = JSON.stringify(draft.sources) !== JSON.stringify(draft.base)
  // A field shows its problem once it has a value, or for every field after an Add attempt.
  const visibleProblem = (field: keyof DebSourceDraft) => (addAttempted || addDraft[field].trim() !== '' ? problems[field] : undefined)

  const close = () => {
    if (!saving) onClose()
  }

  const remove = (name: string) => {
    setDraft((current) => ({ ...current, sources: current.sources.filter((source) => source.name !== name) }))
  }

  const add = () => {
    setAddAttempted(true)
    if (!addValid) return
    const source = debSourceFromDraft(addDraft)
    setDraft((current) => ({ ...current, sources: [...current.sources, source] }))
    setAddDraft(EMPTY_DEB_SOURCE_DRAFT)
    setAddAttempted(false)
  }

  const addOnEnter = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'Enter' || !(event.target instanceof HTMLInputElement)) return
    event.preventDefault()
    add()
  }

  const save = async () => {
    if (!dirty || saving) return
    setSaving(true)
    setError(null)
    try {
      const saved = await localRepository.replaceSpec(siteId, { generation: draft.generation, sources: draft.sources })
      onSaved(saved)
    } catch (caught) {
      if (caught instanceof LocalRepositorySpecConflictError) {
        setConflict('waiting')
        onConflict()
      } else {
        setError(failureOf(caught, 'The user sources could not be saved.'))
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      size="xl"
      closeOnInteractOutside={!saving}
      title="Manage user sources"
      description="Add or remove the sources you asked this Site's Local Repository to mirror. Changes apply when you save; new sources read Pending until the worker syncs them."
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={saving}>
            Cancel
          </Button>
          <Button colorPalette="brand" onClick={() => void save()} loading={saving} disabled={!dirty || saving}>
            Save sources
          </Button>
        </>
      }
    >
      <Stack gap="5">
        {conflict && (
          <Alert status="warning" title="The user sources changed since you opened this dialog">
            {conflict === 'waiting'
              ? 'Nothing was saved. Loading the current sources…'
              : 'Nothing was saved. The list now shows the current sources; make your change again.'}
          </Alert>
        )}
        {error && (
          <Alert status="error" title="User sources could not be saved">
            <FailureMessage failure={error} />
          </Alert>
        )}

        <Box as="section" aria-labelledby="user-sources-current">
          <Heading as="h3" size="sm" id="user-sources-current" mb="2">
            Your sources
          </Heading>
          {draft.sources.length === 0 ? (
            <Text color="fg.muted" fontSize="sm">No user sources. Add one below.</Text>
          ) : (
            <Box as="ul" className="sw-local-repository-dialog__list" aria-label="Your sources">
              {draft.sources.map((source) => (
                <HStack as="li" key={source.name} justify="space-between" align="flex-start" gap="3">
                  <UserSourceSummary source={source} />
                  <Button size="xs" variant="outline" colorPalette="red" onClick={() => remove(source.name)} aria-label={`Remove source ${source.name}`}>
                    <Trash2 size={14} aria-hidden />
                    Remove
                  </Button>
                </HStack>
              ))}
            </Box>
          )}
        </Box>

        {moduleSources.length > 0 && (
          <Box as="details" className="sw-local-repository-dialog__modules">
            <summary>
              <Lock size={14} aria-hidden /> Sources declared by modules ({moduleSources.length}), not editable here
            </summary>
            <Box as="ul" className="sw-local-repository-dialog__list" aria-label="Sources declared by modules">
              {moduleSources.map((source) => (
                <li key={source.name}>
                  <Text fontWeight="semibold">{source.name}</Text>
                  <Text fontSize="xs" color="fg.muted">
                    {source.format} · declared by {declarersText(source.declaredBy)}. It changes with the module, not here.
                  </Text>
                </li>
              ))}
            </Box>
          </Box>
        )}

        <Box as="section" aria-labelledby="user-sources-add" onKeyDown={addOnEnter}>
          <Heading as="h3" size="sm" id="user-sources-add" mb="1">
            Add a Debian or Ubuntu source
          </Heading>
          <Text color="fg.muted" fontSize="sm" mb="3">
            An apt archive the modules do not declare. Leave credentials out of the URL: the spec is shown in plain text.
          </Text>
          <div className="sw-resource-form-grid">
            {ADD_FIELDS.map((item) => {
              const problem = visibleProblem(item.field)
              return (
                <Field.Root key={item.field} required={item.required} invalid={problem !== undefined} disabled={saving}>
                  <Field.Label>
                    {item.label} {item.required && <Field.RequiredIndicator />}
                  </Field.Label>
                  <Input
                    value={addDraft[item.field]}
                    placeholder={item.placeholder}
                    type={item.field === 'url' ? 'url' : 'text'}
                    autoComplete="off"
                    onChange={(event) => setAddDraft((current) => ({ ...current, [item.field]: event.target.value }))}
                  />
                  {problem ? <Field.ErrorText>{problem}</Field.ErrorText> : item.help && <Field.HelperText>{item.help}</Field.HelperText>}
                </Field.Root>
              )
            })}
          </div>
          <HStack justify="flex-end" mt="3">
            <Button size="sm" variant="outline" onClick={add} disabled={saving}>
              <Plus size={14} aria-hidden />
              Add to list
            </Button>
          </HStack>
        </Box>
      </Stack>
    </Modal>
  )
}
