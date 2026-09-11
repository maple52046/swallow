import { useMemo } from 'react'
import { Portal, Select, createListCollection } from '@chakra-ui/react'

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
 * Full-width single-select field whose options render in a portalled listbox
 * rather than the browser's native `<select>` popup.
 *
 * This is the shared select control for forms and toolbars: it keeps keyboard
 * navigation, a visible placeholder, focus restoration on close, and a
 * form-label target through `id`. The `HiddenSelect` keeps native form
 * semantics for required validation. `onChange` reports the selected value, or an
 * empty string once cleared.
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
  // The collection is Chakra's value/label lookup; rebuild it only when options change.
  const collection = useMemo(
    () =>
      createListCollection({
        items: options.map((option) => ({
          label: option.label,
          value: option.value,
          disabled: option.isDisabled ?? false,
        })),
      }),
    [options],
  )

  return (
    <Select.Root
      collection={collection}
      value={value ? [value] : []}
      onValueChange={(details) => onChange(details.value[0] ?? '')}
      disabled={isDisabled}
      required={isRequired}
      positioning={{ sameWidth: true }}
      width="full"
    >
      <Select.HiddenSelect />
      <Select.Control>
        <Select.Trigger id={id} aria-label={ariaLabel}>
          <Select.ValueText placeholder={placeholder} />
        </Select.Trigger>
        <Select.IndicatorGroup>
          <Select.Indicator />
        </Select.IndicatorGroup>
      </Select.Control>
      <Portal>
        <Select.Positioner>
          <Select.Content maxH="20rem">
            {collection.items.map((item) => (
              <Select.Item item={item} key={item.value}>
                <Select.ItemText>{item.label}</Select.ItemText>
                <Select.ItemIndicator />
              </Select.Item>
            ))}
          </Select.Content>
        </Select.Positioner>
      </Portal>
    </Select.Root>
  )
}
