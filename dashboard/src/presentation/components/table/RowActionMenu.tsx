import { DropdownMenu, IconButton } from '@radix-ui/themes'
import { DotsHorizontalIcon } from '@radix-ui/react-icons'

/** One action in a row menu. `color="red"` marks a destructive item. */
export interface RowAction {
  label: string
  onSelect: () => void
  color?: 'red'
  disabled?: boolean
}

/** A titled group of actions, rendered with a separator and label between groups. */
export interface RowActionGroup {
  label?: string
  actions: RowAction[]
}

interface RowActionMenuProps {
  groups: RowActionGroup[]
  /** Accessible label for the trigger, e.g. "Actions for gpu-node-01". */
  ariaLabel: string
  disabled?: boolean
}

/**
 * The per-row "kebab" action menu, replacing MAAS's inline row `TableMenu`.
 *
 * Renders grouped actions in a Radix dropdown. Groups are separated and optionally
 * labelled so power / validation / operator actions stay visually distinct, matching the
 * detail page's action grouping. The trigger carries an `aria-label` because it shows
 * only an icon.
 */
export function RowActionMenu({ groups, ariaLabel, disabled }: RowActionMenuProps) {
  const nonEmpty = groups.filter((group) => group.actions.length > 0)
  if (nonEmpty.length === 0) return null

  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger>
        <IconButton variant="ghost" color="gray" aria-label={ariaLabel} disabled={disabled}>
          <DotsHorizontalIcon />
        </IconButton>
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="end">
        {nonEmpty.map((group, groupIndex) => (
          <DropdownMenu.Group key={group.label ?? groupIndex}>
            {groupIndex > 0 && <DropdownMenu.Separator />}
            {group.label && <DropdownMenu.Label>{group.label}</DropdownMenu.Label>}
            {group.actions.map((action) => (
              <DropdownMenu.Item
                key={action.label}
                color={action.color}
                disabled={action.disabled}
                onSelect={action.onSelect}
              >
                {action.label}
              </DropdownMenu.Item>
            ))}
          </DropdownMenu.Group>
        ))}
      </DropdownMenu.Content>
    </DropdownMenu.Root>
  )
}
