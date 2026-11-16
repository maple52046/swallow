import { Stack, Text } from '@chakra-ui/react'
import { HiddenExperimentalSuites } from './HiddenExperimentalSuites'

interface HiddenSuitesProps {
  /** How many experimental suites the picker left out (`useShowExperimentalSuites`). */
  experimental: number
  /** How many other Users' suites the picker left out (`useHideOtherUsersSuites`). */
  otherUsers: number
}

/**
 * The notes a suite picker shows when its preferences leave suites out, so a User who expects one
 * knows where it went and how to offer it: the switches on the Suites page. Experimental suites use
 * `HiddenExperimentalSuites` (the schedule picker, which offers no user suite, uses it alone); other
 * Users' suites get the matching note. It is helper text of the picker, so screen readers read it
 * with the Suite label. Renders nothing when nothing is hidden.
 */
export function HiddenSuites({ experimental, otherUsers }: HiddenSuitesProps) {
  if (experimental === 0 && otherUsers === 0) return null
  return (
    <Stack gap="1">
      {experimental > 0 && <HiddenExperimentalSuites count={experimental} />}
      {otherUsers > 0 && (
        <Text fontSize="sm" color="fg.muted">
          {otherUsers === 1 ? '1 suite by another User is hidden.' : `${otherUsers} suites by other Users are hidden.`} Turn off Hide
          other users’ suites on the Suites page to offer them.
        </Text>
      )}
    </Stack>
  )
}
