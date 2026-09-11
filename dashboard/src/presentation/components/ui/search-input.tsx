import { CloseButton, Input, InputGroup } from '@chakra-ui/react'
import { Search } from 'lucide-react'

interface SearchInputProps {
  value: string
  onChange: (value: string) => void
  placeholder?: string
  'aria-label': string
  /** Constrains the control width; toolbars typically cap it so it does not sprawl. */
  maxW?: string
  size?: 'sm' | 'md'
}

/**
 * Shared search field with a leading magnifier and a clear affordance.
 *
 * Used across list toolbars for free-text filtering. Exposes a plain
 * `value`/`onChange(value)` contract (the clear button emits an empty string), so
 * callers keep filter state in the URL or local state as they see fit.
 */
export function SearchInput({
  value,
  onChange,
  placeholder,
  maxW = '24rem',
  size = 'sm',
  'aria-label': ariaLabel,
}: SearchInputProps) {
  return (
    <InputGroup
      flex="1"
      maxW={maxW}
      startElement={<Search size={16} />}
      endElement={
        value ? (
          <CloseButton size="xs" aria-label="Clear search" onClick={() => onChange('')} me="-2" />
        ) : undefined
      }
    >
      <Input
        size={size}
        value={value}
        placeholder={placeholder}
        aria-label={ariaLabel}
        onChange={(event) => onChange(event.target.value)}
      />
    </InputGroup>
  )
}
