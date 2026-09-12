import { useState, type ReactNode } from 'react'
import { Box, Button, Flex, Steps, Text } from '@chakra-ui/react'

/** One wizard step: a stable id, a nav label, its body, and whether the operator may advance. */
export interface WizardStepDef {
  id: string
  name: string
  content: ReactNode
  /** When false the Next/Finish control is disabled on this step. Defaults to true. */
  canProceed?: boolean
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
}

/**
 * Linear deployment workspace built on Chakra `Steps`.
 *
 * Desktop uses a stable progress rail beside the active workspace; mobile keeps a
 * compact textual progress marker. Forward movement is gated by the active step,
 * while completed steps remain available for backward navigation.
 */
export function Wizard({ steps, onFinish, finishLabel = 'Finish', finishing = false }: WizardProps) {
  const [current, setCurrent] = useState(0)
  const step = steps[current]
  const isLast = current === steps.length - 1
  const canProceed = step.canProceed !== false
  const goNext = () => setCurrent((value) => Math.min(steps.length - 1, value + 1))

  return (
    <Box className="sw-wizard" colorPalette="brand">
      <Box className="sw-wizard__rail">
        <Text className="sw-wizard__rail-label">Deployment flow</Text>
        <Steps.Root
          step={current}
          count={steps.length}
          orientation="vertical"
          onStepChange={(details) => {
            if (details.step < current || (details.step === current + 1 && canProceed && !step.onNext)) setCurrent(details.step)
          }}
          size="sm"
        >
          <Steps.List className="sw-wizard__steps">
            {steps.map((entry, index) => (
              <Steps.Item key={entry.id} index={index}>
                <Steps.Trigger>
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
        <Box className="sw-wizard__progress" aria-live="polite">
          <Text as="span">Step {current + 1} of {steps.length}</Text>
          <Text as="strong">{step.name}</Text>
        </Box>
        <Box className="sw-wizard__content">{step.content}</Box>

        <Flex className="sw-wizard__actions" justify="space-between" gap="2">
          <Button variant="ghost" disabled={current === 0} onClick={() => setCurrent((value) => Math.max(0, value - 1))}>
            Back
          </Button>
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
        </Flex>
      </Box>
    </Box>
  )
}
