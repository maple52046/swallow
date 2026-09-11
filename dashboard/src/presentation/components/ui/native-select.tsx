import { NativeSelect as ChakraNativeSelect } from '@chakra-ui/react'
import type { ReactNode } from 'react'

interface NativeSelectProps {
  value: string
  onChange: (value: string) => void
  'aria-label': string
  /** `<option>` elements. */
  children: ReactNode
  id?: string
  size?: ChakraNativeSelect.RootProps['size']
  disabled?: boolean
}

/**
 * Shared native `<select>` wrapper for compact filter and form dropdowns.
 *
 * Used where a simple, keyboard-native option list is preferable to the portalled
 * `SingleSelect` (toolbars, dense filter rows). Exposes a plain
 * `value`/`onChange(value)` contract; render the choices as `<option>` children.
 */
export function NativeSelect({
  value,
  onChange,
  children,
  id,
  size = 'sm',
  disabled,
  'aria-label': ariaLabel,
}: NativeSelectProps) {
  return (
    <ChakraNativeSelect.Root size={size} disabled={disabled}>
      <ChakraNativeSelect.Field
        id={id}
        value={value}
        aria-label={ariaLabel}
        onChange={(event) => onChange(event.currentTarget.value)}
      >
        {children}
      </ChakraNativeSelect.Field>
      <ChakraNativeSelect.Indicator />
    </ChakraNativeSelect.Root>
  )
}
