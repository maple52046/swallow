import { Spinner, type SpinnerProps } from '@chakra-ui/react'

/**
 * The small rotating cue placed after a status label while work is still running: the
 * Deployment cell's in-progress OS provisioning states and the OS Images Deploy Mode tags while
 * a verification runs. One component so both surfaces look and move the same.
 *
 * The label next to it carries the state, so the spinner is hidden from assistive technology.
 * It keeps rotating when the operating system asks for reduced motion, by product request: the
 * global reduced-motion rule in `src/index.css` would otherwise freeze it into a static arc that
 * no longer reads as "in progress". The exemption lives on the `sw-progress-spinner` class.
 */
export function InProgressSpinner({ color }: { color?: SpinnerProps['color'] }) {
  return <Spinner size="xs" color={color} className="sw-progress-spinner" aria-hidden />
}
