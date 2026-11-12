import { Field } from '@chakra-ui/react'

/**
 * The note a suite picker shows when it leaves experimental suites out, so an admin who expects one
 * knows where it went and how to offer it: the switch on the Suites page. It is helper text of the
 * picker's field, so screen readers read it with the Suite label. `count` is how many were hidden.
 */
export function HiddenExperimentalSuites({ count }: { count: number }) {
  return (
    <Field.HelperText>
      {count === 1 ? '1 experimental suite is hidden.' : `${count} experimental suites are hidden.`} Turn on Show experimental
      suites on the Suites page to offer them.
    </Field.HelperText>
  )
}
