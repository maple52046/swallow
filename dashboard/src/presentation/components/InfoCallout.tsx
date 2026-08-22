import { Callout } from '@radix-ui/themes'
import { InfoCircledIcon } from '@radix-ui/react-icons'
import type { ReactNode } from 'react'

interface InfoCalloutProps {
  children: ReactNode
}

/**
 * A neutral, informational banner for first-run / onboarding guidance.
 *
 * Deliberately NOT a warning. An empty or not-yet-configured platform is an expected
 * first-run state, not a fault, so it must not look like something is broken. This is the
 * single shared surface for that message (coding-style DRY gate): reuse it instead of
 * hand-rolling a Callout per page, so onboarding notes stay visually distinct from real
 * problems.
 *
 * Accessibility: severity is never conveyed by colour alone. Information uses the neutral
 * gray colour and InfoCircledIcon and is announced with role="status"; genuine problems
 * (for example an integration that is registered but failing to sync) keep the
 * amber/orange ExclamationTriangle callouts and role="alert". The icon, not just the
 * colour, tells the two apart.
 */
export function InfoCallout({ children }: InfoCalloutProps) {
  return (
    <Callout.Root color="gray" variant="surface" mb="4" role="status">
      <Callout.Icon>
        <InfoCircledIcon />
      </Callout.Icon>
      <Callout.Text>{children}</Callout.Text>
    </Callout.Root>
  )
}
