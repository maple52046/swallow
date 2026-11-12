import { useState } from 'react'
import { Badge, Box, Button, Grid, Heading, HStack, Stack, Text } from '@chakra-ui/react'
import { ArrowLeft, ArrowRight, Check, Play } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { moduleSettingsSpec, moduleSettingsValid } from '@/domain/module/settings'
import type { ValidationSuite } from '@/domain/validation/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { ModuleSettingsFields } from '@/presentation/components/ModuleSettings'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useShowExperimentalSuites } from '@/presentation/hooks/useShowExperimentalSuites'
import { HiddenExperimentalSuites } from './HiddenExperimentalSuites'
import { IMPACT_COPY, offeredSuites } from './validationPresentation'
import { ValidationTargetFields } from './ValidationTargetFields'

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
}

/**
 * Shared two-step report-run flow. Suite, target, parameters, impact, and budget are reviewed
 * before the API accepts durable work. A Server or Platform detail can fix the target; Suite detail
 * can fix the suite without forking validation or payload construction.
 */
export function StartValidationRunDialog({ open, onClose, suites, preset }: StartValidationRunDialogProps) {
  const { validation } = useApp()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [showExperimental] = useShowExperimentalSuites()
  const { offered: choices, hiddenExperimental } = offeredSuites(suites, showExperimental, preset?.suiteId)
  const [step, setStep] = useState<1 | 2>(1)
  const [suiteId, setSuiteId] = useState(preset?.suiteId ?? '')
  const [serverIds, setServerIds] = useState<string[]>(preset?.serverIds ?? [])
  const [platformId, setPlatformId] = useState(preset?.platformId ?? '')
  const [parameters, setParameters] = useState<Record<string, string>>({})
  const [confirmed, setConfirmed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [failure, setFailure] = useState<DisplayedFailure | null>(null)
  const suite = choices.find((item) => item.id === suiteId) ?? null
  const suiteFixed = Boolean(preset?.suiteId)

  const chooseSuite = (id: string) => {
    setSuiteId(id)
    setServerIds(preset?.serverIds ?? [])
    setPlatformId(preset?.platformId ?? '')
    setParameters({})
    setConfirmed(false)
    setFailure(null)
  }
  const targetsChosen = suite?.target === 'platform' ? platformId !== '' : serverIds.length > 0
  const parametersValid = !suite?.parameters || moduleSettingsValid(suite.parameters, parameters)
  const reviewReady = suite !== null && targetsChosen
  const submitReady = reviewReady && parametersValid && (suite?.impact !== 'disruptive' || confirmed)

  const submit = async () => {
    if (!suite || !submitReady) return
    setBusy(true)
    setFailure(null)
    try {
      const run = await validation.startRun({
        suiteId: suite.id,
        serverIds: suite.target === 'servers' ? serverIds : undefined,
        platformId: suite.target === 'platform' ? platformId : undefined,
        parameters: suite.parameters ? moduleSettingsSpec(suite.parameters, parameters) : undefined,
        confirm: suite.impact === 'disruptive' ? confirmed : undefined,
      })
      showToast({ tone: 'success', title: 'Validation Run started', description: suite.name })
      onClose()
      navigate(scopedHref(`/validation/runs/${encodeURIComponent(run.id)}`, run.siteId))
    } catch (caught: unknown) {
      setFailure(failureOf(caught, 'The run was not started.'))
    } finally {
      setBusy(false)
    }
  }

  const footer = step === 1 ? (
    <>
      <Button variant="ghost" onClick={onClose}>Cancel</Button>
      <Button colorPalette="brand" onClick={() => setStep(2)} disabled={!reviewReady}>
        Continue <ArrowRight size={16} aria-hidden />
      </Button>
    </>
  ) : (
    <>
      <Button variant="ghost" onClick={() => setStep(1)} disabled={busy}>
        <ArrowLeft size={16} aria-hidden /> Back
      </Button>
      <Button colorPalette="brand" onClick={() => void submit()} loading={busy} disabled={!submitReady}>
        <Play size={16} aria-hidden /> Start run
      </Button>
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
                        onClick={() => chooseSuite(choice.id)}
                      >
                        <Stack gap="1" align="stretch" width="full">
                          <HStack justify="space-between" gap="2">
                            <Text fontWeight="semibold">{choice.name}</Text>
                            {choice.experimental && <Badge colorPalette="orange">Experimental</Badge>}
                          </HStack>
                          <Text color="fg.muted" fontSize="sm">{choice.description || `${choice.checks.length} checks`}</Text>
                          <Text fontSize="xs">{choice.target === 'platform' ? 'Platform' : 'Servers'} · {IMPACT_COPY[choice.impact].label}</Text>
                        </Stack>
                      </Button>
                    )
                  })}
                </Grid>
                {hiddenExperimental > 0 && <HiddenExperimentalSuites count={hiddenExperimental} />}
              </Stack>
            )}
            {suite && (
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
            {!reviewReady && suite && <Text color="fg.muted" fontSize="sm">Choose at least one eligible target to continue.</Text>}
          </Stack>
        )}

        {step === 2 && suite && (
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
            ) : (
              <Box borderWidth="1px" rounded="lg" p="4">
                <Text fontWeight="semibold">No parameters required</Text>
                <Text color="fg.muted" fontSize="sm">This suite runs with its declared checks and budget.</Text>
              </Box>
            )}
            <Box borderWidth="1px" rounded="lg" p="4">
              <Heading as="h3" size="sm" mb="3">Review</Heading>
              <Stack gap="2" fontSize="sm">
                <HStack justify="space-between"><Text color="fg.muted">Suite</Text><Text fontWeight="medium">{suite.name}</Text></HStack>
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
