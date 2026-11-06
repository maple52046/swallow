import { Tabs } from '@chakra-ui/react'

export interface WorkspaceTabItem {
  value: string
  label: string
}

interface WorkspaceTabsProps {
  value: string
  items: readonly WorkspaceTabItem[]
  label: string
  onChange: (value: string) => void
}

/**
 * Route-level navigation for sibling resource workspaces.
 *
 * Presentation is intentionally delegated to Chakra Tabs' default recipe while
 * this component keeps route values and accessible labelling consistent. When the
 * tabs are wider than a narrow viewport the list scrolls sideways (its scrollbar
 * hidden, as on the Integration settings page) instead of widening the page.
 */
export function WorkspaceTabs({ value, items, label, onChange }: WorkspaceTabsProps) {
  return (
    <Tabs.Root value={value} onValueChange={(details) => onChange(details.value)} aria-label={label}>
      <Tabs.List
        maxW="full"
        overflowX="auto"
        overscrollBehaviorX="contain"
        scrollbarWidth="none"
        css={{ '&::-webkit-scrollbar': { display: 'none' } }}
      >
        {items.map((item) => (
          <Tabs.Trigger key={item.value} value={item.value} flex="none">
            {item.label}
          </Tabs.Trigger>
        ))}
      </Tabs.List>
    </Tabs.Root>
  )
}
