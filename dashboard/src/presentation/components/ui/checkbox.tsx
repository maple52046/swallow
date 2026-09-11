import { Checkbox as ChakraCheckbox } from '@chakra-ui/react'
import { forwardRef, type ComponentProps, type ReactNode } from 'react'

export interface CheckboxProps extends Omit<ChakraCheckbox.RootProps, 'checked' | 'onCheckedChange'> {
  /** Controlled checked state; `'indeterminate'` renders the mixed state for select-all controls. */
  checked?: boolean | 'indeterminate'
  /** Fires with the next boolean checked state (indeterminate is coerced to false). */
  onCheckedChange?: (checked: boolean) => void
  /** Visible label; omit for a standalone control that is labelled elsewhere. */
  children?: ReactNode
  inputProps?: ComponentProps<typeof ChakraCheckbox.HiddenInput>
}

/**
 * Shared checkbox wrapping Chakra's compound `Checkbox` with a plain
 * `checked`/`onCheckedChange` API.
 *
 * Used for every boolean form control in the console so label association, focus,
 * and keyboard behaviour stay consistent. The forwarded ref points at the hidden
 * native input, so callers can integrate with focus management or form libraries.
 */
export const Checkbox = forwardRef<HTMLInputElement, CheckboxProps>(function Checkbox(props, ref) {
  const { checked, onCheckedChange, children, inputProps, ...rest } = props
  return (
    <ChakraCheckbox.Root
      checked={checked}
      onCheckedChange={(details) => onCheckedChange?.(details.checked === true)}
      {...rest}
    >
      <ChakraCheckbox.HiddenInput ref={ref} {...inputProps} />
      <ChakraCheckbox.Control />
      {children && <ChakraCheckbox.Label>{children}</ChakraCheckbox.Label>}
    </ChakraCheckbox.Root>
  )
})
