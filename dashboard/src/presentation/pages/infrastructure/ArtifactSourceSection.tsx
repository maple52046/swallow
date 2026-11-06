import { useState } from 'react'
import { Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { LocalRepositorySpecConflictError, type LocalRepository } from '@/domain/localRepository/types'
import { ChoiceCardGroup, type ChoiceCardOption } from '@/presentation/components/ChoiceCardGroup'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'

/** The two values of the setting as the radio group carries them; the spec holds a boolean. */
type ArtifactSource = 'upstream' | 'local-repository'

// The product describes what each choice does and nothing else: no comparison between them.
const ARTIFACT_SOURCE_OPTIONS: ReadonlyArray<ChoiceCardOption<ArtifactSource>> = [
  {
    value: 'upstream',
    label: 'Upstream sources',
    description: 'Each artifact comes from the upstream source its module declares. Servers keep their own OS package sources.',
  },
  {
    value: 'local-repository',
    label: 'This local repository',
    description: "Every artifact comes from this Site's local repository. Deployments are accepted once it is ready.",
  },
]

/** The confirmation of a switch to one choice — what it does — and the toast once it is saved. */
interface SwitchText {
  title: string
  confirmLabel: string
  consequence: string
  savedTitle: string
  savedText: string
}

const SWITCH_TEXT: Record<ArtifactSource, SwitchText> = {
  upstream: {
    title: 'Fetch deployment artifacts from the upstream sources?',
    confirmLabel: 'Use the upstream sources',
    consequence: 'Deployments accepted after this change fetch each artifact from the upstream source its module declares. '
      + 'Swallow stops synchronizing this repository and keeps what it already holds.',
    savedTitle: 'Deployments use the upstream sources',
    savedText: 'Deployments accepted from now on fetch each artifact from the upstream source its module declares.',
  },
  'local-repository': {
    title: 'Fetch deployment artifacts from this local repository?',
    confirmLabel: 'Use this local repository',
    consequence: 'Deployments accepted after this change fetch every artifact from this local repository. '
      + 'Swallow starts synchronizing it now, and deployments are refused until it is ready.',
    savedTitle: 'Deployments use the local repository',
    savedText: 'Swallow synchronizes it now; deployments are accepted once it is ready.',
  },
}

/**
 * A write refused because the spec changed since it was read: `waiting` until the page's re-read
 * brings a generation other than `generation`, then `rebased`, when the choice shown is current.
 */
interface SpecConflict {
  state: 'waiting' | 'rebased'
  generation: number
}

interface ArtifactSourceSectionProps {
  siteId: string
  /** The repository as the page last read it; a re-read after a conflict arrives through this prop. */
  repository: LocalRepository
  /** Receives the stored Local Repository after the setting was saved, so the page shows it at once. */
  onSaved: (repository: LocalRepository) => void
  /** Asks the page to read the repository again after the API refused a stale generation. */
  onConflict: () => void
}

/**
 * The Local repository page's setting for where the Site's deployments fetch their artifacts: the
 * upstream sources the modules declare, or this Local Repository (`spec.serveDeployments`; contract
 * local-repository.md, Replace The Spec and Deployments). The wording only says what each choice
 * does.
 *
 * - The cards show the stored value. Picking the other one selects it and opens a confirmation that
 *   states what the switch does; only confirming writes, and cancelling (or a refused write) selects
 *   the stored value again. The picked card stays selected while the dialog is open so the native
 *   radio state, which a click has already changed, and the cards never disagree. The confirm
 *   button uses the primary colour: the change is consequential, not destructive, and it can be
 *   switched back.
 * - The write replaces the whole spec: the new `serveDeployments` with the user's sources and the
 *   generation of the read the choice was made on, unchanged. A saved switch hands the stored
 *   repository to the page (`onSaved`) and says in a toast what deployments do from now on.
 * - A `409` (someone changed the spec since it was read) writes nothing: the dialog closes, the page
 *   reads the repository again, and a warning says the choice shown is the current one. The cards
 *   stay disabled until that newer generation arrives, so the next pick is made on it. Any other
 *   refusal stays in the dialog with its request ID.
 */
export function ArtifactSourceSection({ siteId, repository, onSaved, onConflict }: ArtifactSourceSectionProps) {
  const { localRepository } = useApp()
  const { showToast } = useToast()
  const [pending, setPending] = useState<ArtifactSource | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)
  const [conflict, setConflict] = useState<SpecConflict | null>(null)

  // Adjusting state during render is React's pattern for reacting to a changed prop: the re-read
  // after a conflict brought a newer generation, so the cards now show the current choice.
  if (conflict?.state === 'waiting' && repository.spec.generation !== conflict.generation) {
    setConflict({ ...conflict, state: 'rebased' })
  }

  const current: ArtifactSource = repository.spec.serveDeployments ? 'local-repository' : 'upstream'
  const text = pending ? SWITCH_TEXT[pending] : null

  const pick = (choice: ArtifactSource) => {
    if (choice === current) return
    setError(null)
    setConflict(null)
    setPending(choice)
  }

  const cancel = () => {
    if (!saving) setPending(null)
  }

  const save = async () => {
    if (!pending || saving) return
    const { generation, sources } = repository.spec
    setSaving(true)
    setError(null)
    try {
      const saved = await localRepository.replaceSpec(siteId, { generation, serveDeployments: pending === 'local-repository', sources })
      setPending(null)
      onSaved(saved)
      showToast({ tone: 'success', title: SWITCH_TEXT[pending].savedTitle, description: SWITCH_TEXT[pending].savedText })
    } catch (caught) {
      if (caught instanceof LocalRepositorySpecConflictError) {
        setPending(null)
        setConflict({ state: 'waiting', generation })
        onConflict()
      } else {
        setError(failureOf(caught, 'The setting could not be saved.'))
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <SectionSurface
      title="Artifact source for deployments"
      description="Where this Site's deployments fetch the packages, images, and files they install."
      className="sw-local-repository-section"
    >
      <Stack gap="3">
        {conflict && (
          <Alert status="warning" title="This Site's Local Repository changed since the page read it">
            {conflict.state === 'waiting'
              ? 'Nothing was saved. Loading the current setting…'
              : 'Nothing was saved. The choice below is the current one; change it again if you still want to.'}
          </Alert>
        )}
        <ChoiceCardGroup
          // The radio group re-syncs the native inputs' checked state only for enabled inputs, so a
          // choice reverted while the cards wait for the re-read after a conflict would stay
          // unchecked; a new generation remounts them from the stored value instead.
          key={repository.spec.generation}
          label="Artifact source for deployments"
          options={ARTIFACT_SOURCE_OPTIONS}
          value={pending ?? current}
          onChange={pick}
          disabled={saving || conflict?.state === 'waiting'}
        />
      </Stack>
      <ConfirmDialog
        open={text !== null}
        title={text?.title ?? ''}
        confirmLabel={text?.confirmLabel ?? ''}
        destructive={false}
        busy={saving}
        onCancel={cancel}
        onConfirm={() => void save()}
      >
        <Stack gap="3">
          <Text>{text?.consequence}</Text>
          <Text color="fg.muted" fontSize="sm">
            Deployments already accepted keep the source they were accepted with. You can switch back at any time.
          </Text>
          {error && (
            <Alert status="error" title="The setting could not be saved">
              <FailureMessage failure={error} />
            </Alert>
          )}
        </Stack>
      </ConfirmDialog>
    </SectionSurface>
  )
}
