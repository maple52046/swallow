import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Box, Button, Flex, Steps, Text } from '@chakra-ui/react'

/** One wizard step: a stable id, a nav label, its body, and whether the operator may advance. */
export interface WizardStepDef {
  id: string
  name: string
  content: ReactNode
  /** When false the Next/Finish control is disabled on this step. Defaults to true. */
  canProceed?: boolean
  /** Concrete guidance displayed beside a disabled Next/Finish action. */
  blockedReason?: string
  /**
   * Optional gate run when Next is pressed. Receives a `proceed` callback the handler must call
   * (typically after async validation) to actually advance. When present, header clicks cannot
   * skip forward past this step.
   */
  onNext?: (proceed: () => void) => void
  /** Overrides the Next button label on this step (e.g. "Check again"). */
  nextLabel?: string
  /** Reflects an in-flight gate on the Next button. */
  nextLoading?: boolean
}

interface WizardProps {
  steps: WizardStepDef[]
  /** Invoked when the operator confirms the final step. */
  onFinish: () => void
  finishLabel?: string
  /** Reflects an in-flight submit on the final step. */
  finishing?: boolean
  /** Optional controlled active step, used by review actions that return to an earlier section. */
  activeStep?: number
  /** Receives backward, forward, and review-driven step navigation. */
  onActiveStepChange?: (step: number) => void
}

/**
 * Linear deployment workspace built on Chakra `Steps`.
 *
 * A horizontal, scrollable step bar keeps the whole deployment sequence above the active
 * workspace at every viewport. Forward movement is gated by the active step, while completed
 * steps remain available for backward navigation.
 */
export function Wizard({
  steps,
  onFinish,
  finishLabel = 'Finish',
  finishing = false,
  activeStep,
  onActiveStepChange,
}: WizardProps) {
  const [internalCurrent, setInternalCurrent] = useState(0)
  const current = Math.min(activeStep ?? internalCurrent, Math.max(steps.length - 1, 0))
  const wizardRef = useRef<HTMLDivElement>(null)
  const progressRef = useRef<HTMLDivElement>(null)
  const activeTriggerRef = useRef<HTMLButtonElement>(null)
  const previousStepRef = useRef(current)
  const step = steps[current]
  const isLast = current === steps.length - 1
  const canProceed = step.canProceed !== false
  const updateCurrent = (next: number | ((value: number) => number)) => {
    const value = typeof next === 'function' ? next(current) : next
    const bounded = Math.max(0, Math.min(steps.length - 1, value))
    if (activeStep === undefined) setInternalCurrent(bounded)
    onActiveStepChange?.(bounded)
  }
  const goNext = () => updateCurrent((value) => value + 1)

  useEffect(() => {
    if (previousStepRef.current === current) return
    previousStepRef.current = current

    // Announce the new step and bring its start below the sticky operator header. This also
    // resets a long dialog workspace to the top without moving deployment data outside React.
    const progress = progressRef.current
    if (!progress) return
    wizardRef.current?.scrollIntoView({ block: 'start' })
    progress.focus({ preventScroll: true })
    activeTriggerRef.current?.scrollIntoView({ block: 'nearest', inline: 'center' })
  }, [current])

  return (
    <Box ref={wizardRef} className="sw-wizard" colorPalette="brand">
      <Box className="sw-wizard__stepbar">
        <Text className="sw-wizard__stepbar-label">Deployment flow</Text>
        <Steps.Root
          step={current}
          count={steps.length}
          orientation="horizontal"
          onStepChange={(details) => {
            if (details.step < current || (details.step === current + 1 && canProceed && !step.onNext)) updateCurrent(details.step)
          }}
          size="sm"
        >
          <Steps.List className="sw-wizard__steps">
            {steps.map((entry, index) => (
              <Steps.Item key={entry.id} index={index}>
                <Steps.Trigger ref={index === current ? activeTriggerRef : undefined}>
                  <Steps.Indicator />
                  <Steps.Title>{entry.name}</Steps.Title>
                </Steps.Trigger>
                <Steps.Separator />
              </Steps.Item>
            ))}
          </Steps.List>
        </Steps.Root>
      </Box>

      <Box className="sw-wizard__workspace">
        <Box ref={progressRef} className="sw-wizard__progress" aria-live="polite" tabIndex={-1}>
          <Text as="span">Step {current + 1} of {steps.length}</Text>
          <Text as="strong">{step.name}</Text>
        </Box>
        <Box className="sw-wizard__content">{step.content}</Box>

        <Flex className="sw-wizard__actions" justify="space-between" gap="2">
          <Button variant="ghost" disabled={current === 0} onClick={() => updateCurrent((value) => value - 1)}>
            Back
          </Button>
          <Box display="grid" justifyItems="end" gap="1" maxW={{ base: '65%', md: '34rem' }}>
            {!canProceed && step.blockedReason && (
              <Text className="sw-wizard__blocked-reason" role="status" textAlign="right">{step.blockedReason}</Text>
            )}
            {isLast ? (
              <Button colorPalette="brand" loading={finishing} disabled={!canProceed || finishing} onClick={onFinish}>
                {finishLabel}
              </Button>
            ) : (
              <Button
                colorPalette="brand"
                loading={step.nextLoading}
                disabled={!canProceed || Boolean(step.nextLoading)}
                onClick={() => (step.onNext ? step.onNext(goNext) : goNext())}
              >
                {step.nextLabel ?? 'Next'}
              </Button>
            )}
          </Box>
        </Flex>
      </Box>
    </Box>
  )
}
