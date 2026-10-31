import { Stack, Text } from '@chakra-ui/react'
import type { Condition } from '@/domain/convergence/types'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { describeConvergence } from './convergencePresentation'

interface ConvergenceStatusProps {
  /** The resource's `status.conditions`, exactly as the API sent them. */
  conditions: readonly Condition[]
  /** The resource's current spec `generation`, to tell a stale judgement from a current one. */
  generation: number
}

/**
 * Whether a migrated resource's provisioner matches what swallow wants (decision 071), for every
 * resource that carries conditions: one labelled badge plus the explanation as visible text, so the
 * state is never conveyed by colour or by a hover-only tooltip. Read-only; the resource's own editor
 * changes the spec, and the backend controller updates the conditions this renders.
 */
export function ConvergenceStatus({ conditions, generation }: ConvergenceStatusProps) {
  const summary = describeConvergence(conditions, generation)
  return (
    <Stack gap="1" align="flex-start">
      <StatusBadge status={summary.badgeStatus} label={summary.label} />
      {summary.detail && (
        <Text color="fg.muted" fontSize="xs">
          {summary.detail}
        </Text>
      )}
    </Stack>
  )
}
