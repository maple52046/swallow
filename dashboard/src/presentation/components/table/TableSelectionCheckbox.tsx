import { Checkbox } from '@radix-ui/themes'

interface TableSelectionCheckboxProps {
  /** `true`/`false`, or `'indeterminate'` for a partially selected header/group. */
  checked: boolean | 'indeterminate'
  onToggle: () => void
  /** Required for assistive tech, since the checkbox has no visible label in the cell. */
  ariaLabel: string
  disabled?: boolean
}

/**
 * A selection checkbox for table headers, group headers, and rows.
 *
 * Wraps Radix `Checkbox` so the tri-state (`indeterminate`) selection used by
 * select-all and per-group headers is expressed one way everywhere, and so every
 * selection control carries an `aria-label` (the cell shows no text label). Selection
 * state is owned by the caller; this only reports toggles.
 */
export function TableSelectionCheckbox({
  checked,
  onToggle,
  ariaLabel,
  disabled,
}: TableSelectionCheckboxProps) {
  return (
    <Checkbox
      checked={checked}
      onCheckedChange={() => onToggle()}
      aria-label={ariaLabel}
      disabled={disabled}
    />
  )
}
