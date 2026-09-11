import { useMemo, type ReactNode } from 'react'
import { Portal, Select as ChakraSelect, createListCollection } from '@chakra-ui/react'
import { useInsideDialog } from './dialog-portal-context'

/** Wraps children in a `Portal` when `enabled`, otherwise renders them inline. */
function MaybePortal({ enabled, children }: { enabled: boolean; children: ReactNode }) {
  return enabled ? <Portal>{children}</Portal> : <>{children}</>
}

/** A single choice in a {@link Select}. `disabled` renders it non-selectable. */
export interface SelectOption {
  readonly value: string
  readonly label: string
  readonly disabled?: boolean
}

interface SelectProps {
  /** Current selected value; an option whose `value` is `''` is a real choice (e.g. "All"). */
  readonly value: string
  /** Emits the newly selected option value (empty string once nothing matches). */
  readonly onChange: (value: string) => void
  readonly options: readonly SelectOption[]
  /** Accessible name; pair with a `Field.Label` via `id` where a visible label exists. */
  readonly 'aria-label': string
  readonly id?: string
  /** Shown when `value` matches no option (the "nothing chosen yet" state). */
  readonly placeholder?: string
  readonly disabled?: boolean
  readonly required?: boolean
  /** `sm` for dense toolbars and in-table rows; `md` (default) for dialog/form fields. */
  readonly size?: ChakraSelect.RootProps['size']
  /** `full` (default) fills the field/cell; toolbars pass `auto` to stay compact. */
  readonly width?: ChakraSelect.RootProps['width']
}

/**
 * The single dropdown control for the console — a themed, DOM-portalled listbox
 * that replaces every native `<select>`.
 *
 * Using Chakra's `Select` (a DOM-portalled `Positioner`) instead of a
 * browser-native `<select>` matters for two reasons: the popup inherits the app
 * theme (native popups are unstyled OS chrome), and it is positioned in-DOM by
 * floating-ui so it anchors under its trigger even inside embedded/webview previews
 * where native popups collapse to the top-left corner. `HiddenSelect` preserves
 * native form semantics (required validation, form submission).
 *
 * Inside a `Modal`, the listbox is rendered inline (not portalled) — signalled by
 * {@link useInsideDialog} — because Chakra's positioner never computes a position
 * when portalled to `document.body` within a focus-trapped dialog, leaving the list
 * parked at the top-left corner. Outside a dialog it portals to the body so it
 * escapes table/overflow clipping.
 *
 * `value`/`onChange` is a plain single-value contract; the caller owns the state
 * and any enum casting. Model a "nothing selected" state with `placeholder`, and a
 * real "all / none" choice as an option whose `value` is `''`.
 */
export function Select({
  value,
  onChange,
  options,
  id,
  placeholder,
  disabled = false,
  required = false,
  size = 'md',
  width = 'full',
  'aria-label': ariaLabel,
}: SelectProps) {
  // Chakra's Select positioner does not compute a position when portalled to
  // document.body from inside a focus-trapped Dialog, so there we render the positioner
  // inline; elsewhere we portal it to the body (escaping table/overflow clipping).
  const insideDialog = useInsideDialog()
  // Chakra's value/label lookup; rebuild only when the option set changes.
  const collection = useMemo(
    () =>
      createListCollection({
        items: options.map((option) => ({
          label: option.label,
          value: option.value,
          disabled: option.disabled ?? false,
        })),
      }),
    [options],
  )

  return (
    <ChakraSelect.Root
      // Register a custom trigger id THROUGH the machine. Putting `id` directly on
      // Select.Trigger overrides zag's generated id, breaking its internal element lookup
      // so it can never measure the trigger — the listbox then never positions and parks
      // at the top-left. `ids.trigger` keeps the id for Field.Label association AND keeps
      // zag's positioning working.
      ids={id ? { trigger: id } : undefined}
      collection={collection}
      // Always pass the value; when it matches no item, ValueText falls back to the placeholder.
      value={[value]}
      onValueChange={(details) => onChange(details.value[0] ?? '')}
      disabled={disabled}
      required={required}
      size={size}
      width={width}
      // Never pin the listbox to the trigger/field width (`sameWidth`): a field in a
      // narrow container (compact toolbars, in-table cells) then forces short options to
      // wrap. The listbox instead sizes to its content, bounded below by the field width
      // and above by a cap (see Content min/max width), so short options stay on one line
      // and only genuinely long ones wrap.
      positioning={{ sameWidth: false }}
    >
      <ChakraSelect.HiddenSelect />
      <ChakraSelect.Control>
        <ChakraSelect.Trigger aria-label={ariaLabel}>
          <ChakraSelect.ValueText placeholder={placeholder} />
        </ChakraSelect.Trigger>
        <ChakraSelect.IndicatorGroup>
          <ChakraSelect.Indicator />
        </ChakraSelect.IndicatorGroup>
      </ChakraSelect.Control>
      <MaybePortal enabled={!insideDialog}>
        <ChakraSelect.Positioner>
          <ChakraSelect.Content
            maxH="20rem"
            // Fit the widest option, but never narrower than the field (`--reference-width`,
            // which Chakra sets from the trigger) and never wider than the cap — beyond the
            // cap a truly long option wraps rather than running off-screen.
            width="max-content"
            minW="var(--reference-width)"
            maxW="min(44rem, 92vw)"
          >
            {collection.items.map((item) => (
              <ChakraSelect.Item item={item} key={item.value}>
                <ChakraSelect.ItemText>{item.label}</ChakraSelect.ItemText>
                <ChakraSelect.ItemIndicator />
              </ChakraSelect.Item>
            ))}
          </ChakraSelect.Content>
        </ChakraSelect.Positioner>
      </MaybePortal>
    </ChakraSelect.Root>
  )
}
