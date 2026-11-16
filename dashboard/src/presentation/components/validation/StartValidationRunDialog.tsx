import { useRef, useState } from 'react'
import { Badge, Box, Button, Grid, Heading, HStack, Stack, Text } from '@chakra-ui/react'
import { ArrowLeft, ArrowRight, Check, Play } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { suiteChangeSince, suiteChangeSinceLastRun, type SuiteChange } from '@/application/usecases/validation/reviewSuiteChange'
import { moduleSettingsSpec, moduleSettingsValid } from '@/domain/module/settings'
import { suiteStartBlocker } from '@/domain/validation/access'
import { ValidationConflictError, type ValidationSuite } from '@/domain/validation/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ModuleSettingsFields } from '@/presentation/components/ModuleSettings'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useHideOtherUsersSuites } from '@/presentation/hooks/useHideOtherUsersSuites'
import { useShowExperimentalSuites } from '@/presentation/hooks/useShowExperimentalSuites'
import { HiddenSuites } from './HiddenSuites'
import { SuiteChangeReview } from './SuiteChangeReview'
import { UserSuiteSummary } from './UserSuiteSummary'
import { IMPACT_COPY, offeredSuites, START_BLOCKER_TEXT, suiteAuthorName } from './validationPresentation'
import { ValidationTargetFields } from './ValidationTargetFields'

/** What the dialog opens with: a fixed suite (Suite detail) or fixed targets (a Server or Platform page). */
export interface StartRunPreset {
  suiteId?: string
  serverIds?: string[]
  platformId?: string
}

interface StartValidationRunDialogProps {
  open: boolean
  onClose: () => void
  suites: readonly ValidationSuite[]
  preset?: StartRunPreset
  /**
   * Called with the suite as the API now holds it when a refused start showed that it has a newer
   * revision, so the page behind the dialog can read it again instead of showing the old one.
   */
  onSuiteChanged?: (suite: ValidationSuite) => void
}

/**
 * The review gate of a user suite's step 2: reading whether it changed since the User's last
 * run, a failed read (no way on until a retry succeeds), or the answer. `change` is null when there
 * is nothing to review; `reason` says why a change is shown.
 */
type ReviewState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; change: SuiteChange | null; reason: 'last-run' | 'changed-while-open' }

/**
 * Shared two-step report-run flow. Suite, target, parameters, impact, and budget are reviewed
 * before the API accepts durable work. A Server or Platform detail can fix the target; Suite detail
 * can fix the suite without forking validation or payload construction.
 *
 * Who may start what (validation contract, Start) shapes the picker: a suite the signed-in User
 * cannot start anywhere is listed but disabled with its reason in words, and targets below the
 * suite's starter level cannot be chosen. For a user suite, step 2 first reads whether the suite
 * changed since the User's own last run of it; if so it shows `SuiteChangeReview` and "Use the new
 * version" is the only way on. The review then shows the script, its author and revision, and that
 * it runs as the User's own account, and the start pins that revision (`suiteRevision`). When the
 * API refuses the start with `409`, the suite is read again: a new revision opens the same review
 * and is handed to `onSuiteChanged`, anything else is shown as the API's message. The dialog is remounted per opening (a new `key`).
 */
export function StartValidationRunDialog({ open, onClose, suites, preset, onSuiteChanged }: StartValidationRunDialogProps) {
  const { validation } = useApp()
  const navigate = useNavigate()
  const { currentUser } = useAuth()
  const { scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [showExperimental] = useShowExperimentalSuites()
  const [hideOthers] = useHideOtherUsersSuites()
  // A suite read again after a refused start replaces the catalog's copy for the rest of the dialog.
  const [refreshed, setRefreshed] = useState<ValidationSuite | null>(null)
  const catalog = refreshed ? suites.map((item) => (item.id === refreshed.id ? refreshed : item)) : suites
  const { offered: choices, hiddenExperimental, hiddenOtherUsers } = offeredSuites(catalog, {
    showExperimental,
    hideOthersThan: hideOthers ? currentUser?.id : undefined,
    keepId: preset?.suiteId,
  })
  const [step, setStep] = useState<1 | 2>(1)
  const [suiteId, setSuiteId] = useState(preset?.suiteId ?? '')
  const [serverIds, setServerIds] = useState<string[]>(preset?.serverIds ?? [])
  const [platformId, setPlatformId] = useState(preset?.platformId ?? '')
  const [parameters, setParameters] = useState<Record<string, string>>({})
  const [confirmed, setConfirmed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<DisplayedFailure | null>(null)
  const [review, setReview] = useState<ReviewState>({ status: 'ready', change: null, reason: 'last-run' })
  // The revision the User agreed to in a review; the start pins the suite's current revision otherwise.
  const [acceptedRevision, setAcceptedRevision] = useState<number | null>(null)
  // Drops the answer of a review read that a newer one (Back, then Continue) replaced.
  const reviewTicket = useRef(0)
  const suite = choices.find((item) => item.id === suiteId) ?? null
  const suiteFixed = Boolean(preset?.suiteId)
  const userSuite = suite?.source === 'user'

  const chooseSuite = (id: string) => {
    setSuiteId(id)
    setServerIds(preset?.serverIds ?? [])
    setPlatformId(preset?.platformId ?? '')
    setParameters({})
    setConfirmed(false)
    setFailure(null)
    setAcceptedRevision(null)
  }

  // Reads whether the chosen user suite changed since the User's last run; a module suite needs no review.
  const loadReview = (target: ValidationSuite) => {
    const mine = ++reviewTicket.current
    if (target.source !== 'user') {
      setReview({ status: 'ready', change: null, reason: 'last-run' })
      return
    }
    setReview({ status: 'loading' })
    suiteChangeSinceLastRun(validation, target).then(
      (change) => {
        if (mine === reviewTicket.current) setReview({ status: 'ready', change, reason: 'last-run' })
      },
      (caught: unknown) => {
        if (mine === reviewTicket.current) setReview({ status: 'error', ...failureOf(caught, 'Could not check whether the suite changed since your last run.') })
      },
    )
  }

  const continueToReview = () => {
    if (!suite) return
    setStep(2)
    setFailure(null)
    loadReview(suite)
  }

  const reviewing = userSuite && review.status === 'ready' && review.change !== null && acceptedRevision !== review.change.current.revision
  const reviewSettled = !userSuite || (review.status === 'ready' && !reviewing)
  const targetsChosen = suite?.target === 'platform' ? platformId !== '' : serverIds.length > 0
  const parametersValid = !suite?.parameters || moduleSettingsValid(suite.parameters, parameters)
  const blocker = suite ? suiteStartBlocker(currentUser, suite) : null
  const reviewReady = suite !== null && targetsChosen && blocker === null
  const submitReady = reviewReady && reviewSettled && parametersValid && (suite?.impact !== 'disruptive' || confirmed)

  // After a 409, reads the suite again: a new revision is reviewed in place, anything else is the API's refusal.
  const recoverFromConflict = async (pinned: number, conflict: ValidationConflictError) => {
    if (!suite) return
    const latest = await validation.getSuite(suite.id).catch(() => null)
    if (!latest || latest.retired || latest.revision === null || latest.revision === pinned) {
      setFailure(failureOf(conflict, 'The run was not started.'))
      return
    }
    const change = await suiteChangeSince(validation, latest, pinned)
    setRefreshed(latest)
    onSuiteChanged?.(latest)
    setAcceptedRevision(null)
    setReview({ status: 'ready', change, reason: 'changed-while-open' })
  }

  const submit = async () => {
    if (!suite || !submitReady) return
    setBusy(true)
    setFailure(null)
    const pinned = suite.source === 'user' ? suite.revision ?? undefined : undefined
    try {
      const run = await validation.startRun({
        suiteId: suite.id,
        serverIds: suite.target === 'servers' ? serverIds : undefined,
        platformId: suite.target === 'platform' ? platformId : undefined,
        parameters: suite.parameters ? moduleSettingsSpec(suite.parameters, parameters) : undefined,
        confirm: suite.impact === 'disruptive' ? confirmed : undefined,
        suiteRevision: pinned,
      })
      showToast({ tone: 'success', title: 'Validation Run started', description: suite.name })
      onClose()
      navigate(scopedHref(`/validation/runs/${encodeURIComponent(run.id)}`, run.siteId))
    } catch (caught: unknown) {
      if (caught instanceof ValidationConflictError && pinned !== undefined) {
        await recoverFromConflict(pinned, caught).catch((reread: unknown) => setFailure(failureOf(reread, 'The run was not started.')))
      } else {
        setFailure(failureOf(caught, 'The run was not started.'))
      }
    } finally {
      setBusy(false)
    }
  }

  const back = () => {
    reviewTicket.current += 1
    setStep(1)
  }

  const footer = step === 1 ? (
    <>
      <Button variant="ghost" onClick={onClose}>Cancel</Button>
      <Button colorPalette="brand" onClick={continueToReview} disabled={!reviewReady}>
        Continue <ArrowRight size={16} aria-hidden />
      </Button>
    </>
  ) : (
    <>
      <Button variant="ghost" onClick={back} disabled={busy}>
        <ArrowLeft size={16} aria-hidden /> Back
      </Button>
      {reviewing && review.status === 'ready' && review.change ? (
        <Button colorPalette="brand" onClick={() => setAcceptedRevision(review.change?.current.revision ?? null)}>
          <Check size={16} aria-hidden /> Use the new version
        </Button>
      ) : (
        <Button colorPalette="brand" onClick={() => void submit()} loading={busy} disabled={!submitReady}>
          <Play size={16} aria-hidden /> Start run
        </Button>
      )}
    </>
  )

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Run a suite"
      description={step === 1 ? 'Choose the assurance capability and its targets.' : 'Configure the suite and review the work before it starts.'}
      size="xl"
      dismissDisabled={busy}
      footer={footer}
    >
      <Stack gap="5">
        <HStack gap="3" aria-label="Run steps">
          {[
            { value: 1, label: 'Suite & targets' },
            { value: 2, label: 'Configuration & review' },
          ].map((item) => (
            <HStack key={item.value} flex="1" gap="2" color={step === item.value ? 'brand.fg' : 'fg.muted'}>
              <Box
                display="grid"
                placeItems="center"
                boxSize="7"
                rounded="full"
                bg={step === item.value ? 'brand.subtle' : step > item.value ? 'green.subtle' : 'bg.muted'}
                fontWeight="bold"
                fontSize="xs"
              >
                {step > item.value ? <Check size={14} aria-hidden /> : item.value}
              </Box>
              <Text fontSize="sm" fontWeight={step === item.value ? 'semibold' : 'medium'}>{item.label}</Text>
            </HStack>
          ))}
        </HStack>

        {step === 1 && (
          <Stack gap="5">
            {suiteFixed && suite ? (
              <Box borderWidth="1px" borderColor="brand.muted" bg="brand.subtle" rounded="lg" p="4">
                <HStack justify="space-between" gap="3" align="flex-start">
                  <Stack gap="1">
                    <Heading as="h3" size="sm">{suite.name}</Heading>
                    <Text fontSize="sm" color="fg.muted">{suite.description}</Text>
                  </Stack>
                  <Badge variant="outline">{IMPACT_COPY[suite.impact].label}</Badge>
                </HStack>
              </Box>
            ) : (
              <Stack gap="2">
                <Text fontWeight="semibold">Suite</Text>
                <Grid templateColumns={{ base: '1fr', md: 'repeat(2, minmax(0, 1fr))' }} gap="3" role="radiogroup" aria-label="Validation Suite">
                  {choices.map((choice) => {
                    const selected = choice.id === suiteId
                    const choiceBlocker = suiteStartBlocker(currentUser, choice)
                    return (
                      <Button
                        key={choice.id}
                        variant="plain"
                        height="auto"
                        minH="7rem"
                        justifyContent="stretch"
                        textAlign="start"
                        borderWidth="1px"
                        borderColor={selected ? 'brand.muted' : 'border'}
                        bg={selected ? 'brand.subtle' : 'bg.panel'}
                        p="4"
                        whiteSpace="normal"
                        role="radio"
                        aria-checked={selected}
                        disabled={choiceBlocker !== null}
                        onClick={() => chooseSuite(choice.id)}
                      >
                        <Stack gap="1" align="stretch" width="full">
                          <HStack justify="space-between" gap="2">
                            <Text fontWeight="semibold">{choice.name}</Text>
                            {choice.experimental && <Badge colorPalette="orange">Experimental</Badge>}
                          </HStack>
                          <Text color="fg.muted" fontSize="sm">{choice.description || `${choice.checks.length} checks`}</Text>
                          <Text fontSize="xs">
                            {choice.target === 'platform' ? 'Platform' : 'Servers'} · {IMPACT_COPY[choice.impact].label}
                            {choice.source === 'user' ? ` · Script by ${suiteAuthorName(choice)}` : ''}
                          </Text>
                          {choiceBlocker && <Text fontSize="xs" color="fg.muted">{START_BLOCKER_TEXT[choiceBlocker]}</Text>}
                        </Stack>
                      </Button>
                    )
                  })}
                </Grid>
                <HiddenSuites experimental={hiddenExperimental} otherUsers={hiddenOtherUsers} />
              </Stack>
            )}
            {suite && blocker && <Text color="fg.muted" fontSize="sm">{START_BLOCKER_TEXT[blocker]}</Text>}
            {suite && !blocker && (
              <ValidationTargetFields
                key={suite.id}
                suite={suite}
                serverIds={serverIds}
                onServerIdsChange={setServerIds}
                platformId={platformId}
                onPlatformIdChange={setPlatformId}
                idPrefix="validation-run"
                disabled={busy}
              />
            )}
            {!reviewReady && suite && !blocker && <Text color="fg.muted" fontSize="sm">Choose at least one eligible target to continue.</Text>}
          </Stack>
        )}

        {step === 2 && suite && userSuite && review.status === 'loading' && (
          <Stack gap="2" role="status">
            <Text fontSize="sm" color="fg.muted">Checking whether the suite changed since your last run…</Text>
            <LoadingState rows={3} />
          </Stack>
        )}
        {step === 2 && suite && userSuite && review.status === 'error' && (
          <Stack gap="3" align="flex-start">
            <FailureMessage failure={review} />
            <Button size="sm" variant="outline" onClick={() => loadReview(suite)}>Try again</Button>
          </Stack>
        )}
        {step === 2 && suite && reviewing && review.status === 'ready' && review.change && (
          <Stack gap="4">
            <SuiteChangeReview change={review.change} reason={review.reason} />
            {failure && <FailureMessage failure={failure} />}
          </Stack>
        )}

        {step === 2 && suite && reviewSettled && (
          <Stack gap="5">
            {suite.parameters && Object.keys(suite.parameters.properties).length > 0 ? (
              <Stack gap="3">
                <Heading as="h3" size="sm">Configuration</Heading>
                <ModuleSettingsFields
                  schema={suite.parameters}
                  value={(name) => parameters[name] ?? ''}
                  onChange={(name, text) => setParameters((current) => ({ ...current, [name]: text }))}
                  idPrefix={`validation-run-${suite.id}`}
                  disabled={busy}
                />
              </Stack>
            ) : !userSuite && (
              <Box borderWidth="1px" rounded="lg" p="4">
                <Text fontWeight="semibold">No parameters required</Text>
                <Text color="fg.muted" fontSize="sm">This suite runs with its declared checks and budget.</Text>
              </Box>
            )}
            {userSuite && (
              <Box borderWidth="1px" rounded="lg" p="4">
                <Heading as="h3" size="sm" mb="3">Script suite</Heading>
                <UserSuiteSummary suite={suite} starterUsername={currentUser?.username} />
              </Box>
            )}
            <Box borderWidth="1px" rounded="lg" p="4">
              <Heading as="h3" size="sm" mb="3">Review</Heading>
              <Stack gap="2" fontSize="sm">
                <HStack justify="space-between"><Text color="fg.muted">Suite</Text><Text fontWeight="medium">{suite.name}</Text></HStack>
                {userSuite && <HStack justify="space-between"><Text color="fg.muted">Revision</Text><Text fontWeight="medium">{suite.revision}</Text></HStack>}
                <HStack justify="space-between"><Text color="fg.muted">Targets</Text><Text fontWeight="medium">{suite.target === 'servers' ? `${serverIds.length} Servers` : '1 Platform'}</Text></HStack>
                <HStack justify="space-between"><Text color="fg.muted">Checks</Text><Text fontWeight="medium">{suite.checks.length}</Text></HStack>
                <HStack justify="space-between"><Text color="fg.muted">Impact</Text><Text fontWeight="medium">{IMPACT_COPY[suite.impact].label}</Text></HStack>
                <HStack justify="space-between"><Text color="fg.muted">Budget</Text><Text fontWeight="medium">{suite.budgetSeconds} seconds</Text></HStack>
              </Stack>
              <Text color="fg.muted" fontSize="sm" mt="3">{IMPACT_COPY[suite.impact].description}</Text>
            </Box>
            {suite.impact === 'disruptive' && (
              <Checkbox checked={confirmed} onCheckedChange={setConfirmed} disabled={busy}>
                I understand this suite may interrupt service on its targets.
              </Checkbox>
            )}
            {failure && <FailureMessage failure={failure} />}
          </Stack>
        )}
      </Stack>
    </Modal>
  )
}
