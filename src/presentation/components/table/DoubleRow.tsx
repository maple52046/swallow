import { Flex, Text } from '@radix-ui/themes'
import type { ReactNode } from 'react'

interface DoubleRowProps {
  primary: ReactNode
  /** Optional muted second line, e.g. MAC under FQDN or architecture under cores. */
  secondary?: ReactNode
  /** Right-align for numeric columns (cores, RAM, storage), matching MAAS. */
  align?: 'start' | 'end'
}

/**
 * A two-line table cell: a primary value with an optional muted secondary line beneath.
 *
 * This is the MAAS machines-table cell shape (e.g. "FQDN / MAC", "Cores / Arch"), factored
 * into one component so every double-value column renders consistently. Presentational
 * only; callers pass already-formatted nodes.
 */
export function DoubleRow({ primary, secondary, align = 'start' }: DoubleRowProps) {
  return (
    <Flex direction="column" align={align} gap="0">
      <Text size="2">{primary}</Text>
      {secondary !== undefined && secondary !== null && secondary !== '' && (
        <Text size="1" color="gray">
          {secondary}
        </Text>
      )}
    </Flex>
  )
}
