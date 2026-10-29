import { Button, IconButton, Menu, Portal } from '@chakra-ui/react'
import { ChevronDown, Trash2 } from 'lucide-react'
import { Tooltip } from '@/presentation/components/ui/tooltip'

interface PlatformRemovalSplitButtonProps {
  /** The primary label (`Uninstall platform`, `Uninstall` in the bulk toolbar, a dialog's submit label). */
  primaryLabel: string
  /**
   * A qualifier appended to the label (`selected` or `eligible` in the bulk toolbar). It is hidden
   * on phone-width toolbars, where the full label would push the chevron out of view; the visible
   * text then stays a prefix of the accessible name.
   */
  primaryLabelSuffix?: string
  /**
   * Why the primary action cannot run now (running uninstall, locked target, unreadable protection).
   * Only the primary part is disabled; the menu item stays available.
   */
  primaryDisabledReason?: string
  /** Disables both parts without a reason, e.g. a dialog submit still waiting for its typed confirmation. */
  disabled?: boolean
  /** Shows the primary part busy and disables both parts while a request is in flight. */
  loading?: boolean
  /** `submit` makes the primary part the dialog form's submit button, so Enter takes the primary action. */
  primaryType?: 'button' | 'submit'
  onPrimaryAction?: () => void
  /** The single menu item (`Delete only…`, or the dialog's "… and delete record" variant). */
  menuItemLabel: string
  onMenuSelect: () => void
  /** Accessible name of the chevron. */
  menuTriggerLabel?: string
  size?: 'sm' | 'md'
  /**
   * Where the control sits: the menu opens below in a page header and above in the bottom toolbar
   * and in a dialog footer. Inside a dialog the menu is not portalled, so it stays within the
   * dialog's focus trap.
   */
  placement: 'header' | 'toolbar' | 'dialog'
}

/**
 * The red split control of the Platform removal flow (decision 010, 2026-10-29 amendment): a primary
 * action with one more destructive variant behind an attached chevron, so the variant is never
 * offered as a peer of the primary action. The detail header and bulk toolbar use it as Uninstall
 * with "Delete only…" in the menu; the Uninstall dialogs use it as their submit button with
 * "… and delete record" in the menu.
 *
 * Accessibility: two native buttons in tab order (primary, then the chevron named by
 * `menuTriggerLabel`); the menu follows the standard menu keyboard model and returns focus to the
 * chevron on Escape. A primary disabled with a reason keeps it in the tooltip and in its accessible
 * name ("Uninstall platform: <reason>"), the same pattern as Repair deployment. The span wrapper
 * exists only so the tooltip still opens over a disabled button.
 */
export function PlatformRemovalSplitButton({
  primaryLabel,
  primaryLabelSuffix,
  primaryDisabledReason,
  disabled = false,
  loading = false,
  primaryType = 'button',
  onPrimaryAction,
  menuItemLabel,
  onMenuSelect,
  menuTriggerLabel = 'More removal options',
  size = 'md',
  placement,
}: PlatformRemovalSplitButtonProps) {
  const fullLabel = primaryLabelSuffix ? `${primaryLabel} ${primaryLabelSuffix}` : primaryLabel
  return (
    <Menu.Root positioning={{ placement: placement === 'header' ? 'bottom-end' : 'top-end' }}>
      <div className="sw-removal-split" data-placement={placement}>
        <Tooltip content={primaryDisabledReason} disabled={!primaryDisabledReason}>
          <span className="sw-removal-split__primary">
            <Button
              type={primaryType}
              colorPalette="red"
              size={size}
              loading={loading}
              disabled={disabled || loading || Boolean(primaryDisabledReason)}
              aria-label={primaryDisabledReason ? `${fullLabel}: ${primaryDisabledReason}` : undefined}
              onClick={onPrimaryAction}
            >
              {/* One text run, so the button's flex gap does not widen the space before the suffix. */}
              <span>
                {primaryLabel}
                {primaryLabelSuffix && <span className="sw-removal-split__suffix">{` ${primaryLabelSuffix}`}</span>}
              </span>
            </Button>
          </span>
        </Tooltip>
        <Menu.Trigger asChild>
          <IconButton
            className="sw-removal-split__toggle"
            colorPalette="red"
            size={size}
            disabled={disabled || loading}
            aria-label={menuTriggerLabel}
          >
            <ChevronDown size={16} aria-hidden />
          </IconButton>
        </Menu.Trigger>
      </div>
      <Portal disabled={placement === 'dialog'}>
        <Menu.Positioner>
          <Menu.Content>
            <Menu.Item value="secondary" color="red.fg" onSelect={onMenuSelect}>
              <Trash2 size={16} aria-hidden />
              {menuItemLabel}
            </Menu.Item>
          </Menu.Content>
        </Menu.Positioner>
      </Portal>
    </Menu.Root>
  )
}
