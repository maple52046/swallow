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
 * this component keeps route values and accessible labelling consistent.
 */
export function WorkspaceTabs({ value, items, label, onChange }: WorkspaceTabsProps) {
  return (
    <Tabs.Root value={value} onValueChange={(details) => onChange(details.value)} aria-label={label}>
      <Tabs.List>
        {items.map((item) => (
          <Tabs.Trigger key={item.value} value={item.value}>
            {item.label}
          </Tabs.Trigger>
        ))}
      </Tabs.List>
    </Tabs.Root>
  )
}
