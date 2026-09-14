import type { ReactNode } from 'react'
import { IconButton } from '@chakra-ui/react'
import { KeyRound, Pencil, Trash2 } from 'lucide-react'
import { Tooltip } from '@/presentation/components/ui/tooltip'

/**
 * The kind of a row action. The kind — not the caller — decides the icon and whether the action is
 * destructive, so the same concept looks identical on every Infrastructure page.
 */
export type ResourceRowActionKind = 'edit' | 'credential' | 'delete'

/** One icon action in a resource row. */
export interface ResourceRowAction {
  kind: ResourceRowActionKind
  /**
   * Accessible name and tooltip text. Must name the action and its target (for example
   * "Edit rack-a"), because the button is icon-only and this is the only label hover, keyboard
   * focus, and assistive tech receive.
   */
  label: string
  onClick: () => void
}

// Centralized icon and severity per kind, so "edit" is the same glyph and "delete" is the same red
// everywhere the group is used.
const ACTION_ICONS: Record<ResourceRowActionKind, ReactNode> = {
  edit: <Pencil size={18} />,
  credential: <KeyRound size={18} />,
  delete: <Trash2 size={18} />,
}
const DESTRUCTIVE_KINDS: Record<ResourceRowActionKind, boolean> = {
  edit: false,
  credential: false,
  delete: true,
}

/**
 * Compact icon-button action group for one resource row.
 *
 * Shared across the Infrastructure registries (Sites, Integrations, Zones, Pools) so every row and
 * mobile card offers the same affordances from one implementation. Each control is icon-only for
 * density but always carries an `aria-label` and a matching tooltip, so the action name is
 * available on hover, on keyboard focus, and to assistive tech — the icon never conveys meaning by
 * shape or color alone. Destructive actions still open their own confirmation dialog; this only
 * renders the trigger.
 */
export function ResourceRowActions({ actions }: { actions: ResourceRowAction[] }) {
  return (
    <span className="sw-row-actions">
      {actions.map((action) => (
        <Tooltip key={action.label} content={action.label}>
          <IconButton
            variant="ghost"
            size="sm"
            colorPalette={DESTRUCTIVE_KINDS[action.kind] ? 'red' : undefined}
            aria-label={action.label}
            onClick={action.onClick}
          >
            {ACTION_ICONS[action.kind]}
          </IconButton>
        </Tooltip>
      ))}
    </span>
  )
}
