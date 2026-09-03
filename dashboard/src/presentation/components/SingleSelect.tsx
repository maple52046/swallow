import { useState } from 'react'
import {
  MenuToggle,
  Select,
  SelectList,
  SelectOption,
} from '@patternfly/react-core'

interface SingleSelectOption {
  readonly value: string
  readonly label: string
  readonly isDisabled?: boolean
}

interface SingleSelectProps {
  readonly id: string
  readonly ariaLabel: string
  readonly value: string
  readonly placeholder: string
  readonly options: readonly SingleSelectOption[]
  readonly isDisabled?: boolean
  readonly isRequired?: boolean
  readonly onChange: (value: string) => void
}

/**
 * Renders a full-width PatternFly single-select field whose options remain in
 * the document rather than in the browser's native select popup.
 *
 * Linux Chromium can paint disabled native placeholder options with an
 * unreadable user-agent color until pointer movement triggers a repaint. This
 * shared control avoids that inaccessible rendering path while preserving a
 * visible disabled placeholder, keyboard navigation, focus restoration, and a
 * form-label target through `id`.
 */
export function SingleSelect({
  id,
  ariaLabel,
  value,
  placeholder,
  options,
  isDisabled = false,
  isRequired = false,
  onChange,
}: SingleSelectProps) {
  const [isOpen, setIsOpen] = useState(false)
  const selectedOption = options.find((option) => option.value === value)

  return (
    <Select
      id={`${id}-menu`}
      isOpen={isOpen}
      selected={selectedOption?.value}
      onOpenChange={setIsOpen}
      onSelect={(_event, selectedValue) => {
        if (typeof selectedValue !== 'string') return
        onChange(selectedValue)
        setIsOpen(false)
      }}
      shouldFocusFirstItemOnOpen
      shouldFocusToggleOnSelect
      maxMenuHeight="20rem"
      popperProps={{ width: 'trigger' }}
      toggle={(toggleRef) => (
        <MenuToggle
          ref={toggleRef}
          id={id}
          type="button"
          aria-label={ariaLabel}
          aria-required={isRequired || undefined}
          isDisabled={isDisabled}
          isExpanded={isOpen}
          isFullWidth
          isInForm
          isPlaceholder={!selectedOption}
          onClick={() => setIsOpen((open) => !open)}
        >
          {selectedOption?.label ?? placeholder}
        </MenuToggle>
      )}
    >
      <SelectList>
        <SelectOption value="" isDisabled>{placeholder}</SelectOption>
        {options.map((option) => (
          <SelectOption key={option.value} value={option.value} isDisabled={option.isDisabled}>
            {option.label}
          </SelectOption>
        ))}
      </SelectList>
    </Select>
  )
}
