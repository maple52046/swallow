import { useLayoutEffect, useRef } from 'react'
import { Tabs } from '@chakra-ui/react'
import type { LucideIcon } from 'lucide-react'

/**
 * One route-backed tab. `value` is the caller's stable deep-link key (a path or a route segment)
 * and is what `onChange` receives; `label` is the visible text and the tab's accessible name.
 */
export interface WorkspaceTabItem {
  value: string
  label: string
  /** Optional glyph before the label; decorative, so the label alone names the tab. */
  icon?: LucideIcon
}

interface WorkspaceTabsProps {
  /**
   * The item the current URL selects. A value no item has (for example a Not Found child route)
   * selects nothing; the first tab then stays in the keyboard tab order.
   */
  value: string
  items: readonly WorkspaceTabItem[]
  /**
   * Accessible name of the tab list, announced when focus enters it. It also identifies the list
   * across pages for the focus hand-off below, so each area uses its own label.
   */
  label: string
  /** Called with the chosen item's value; callers navigate, so the URL stays the source of truth. */
  onChange: (value: string) => void
}

// The label of a tab list that held focus as it unmounted. Area tabs (Infrastructure, OS Images,
// Activity) are rendered by each sibling page, so choosing a tab unmounts the list with its page
// and mounts an identical one on the next page in the same commit; the new list takes focus back
// so a keyboard user keeps their place. A deliberate module-level hand-off between two instances:
// it is cleared after the commit, so a list mounted later never steals focus.
let focusHandoff: string | null = null

/**
 * Route-level navigation for sibling workspaces: the area tabs under a page header (Infrastructure,
 * OS Images, Activity) and the tabs of a detail page (Server, Platform).
 *
 * Presentation is intentionally delegated to Chakra Tabs' default recipe while this component keeps
 * route values, icons, and accessible labelling consistent. Selection is controlled by the caller
 * from the URL; arrow keys move between tabs and select them, which navigates, and focus stays on
 * the selected tab even when the navigation renders a different page. When the tabs are wider than
 * a narrow viewport the list scrolls sideways (its scrollbar hidden) instead of widening the page,
 * every tab keeps its natural width, and the selected tab is scrolled into view.
 */
export function WorkspaceTabs({ value, items, label, onChange }: WorkspaceTabsProps) {
  const listRef = useRef<HTMLDivElement>(null)
  const selectsNothing = !items.some((item) => item.value === value)

  // A layout effect, because its cleanup runs before React removes the list from the document, so
  // it can still see that focus is inside it; the next page's list mounts later in the same commit.
  useLayoutEffect(() => {
    const list = listRef.current
    if (focusHandoff === label) {
      focusHandoff = null
      list?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')?.focus()
    }
    return () => {
      if (!list?.contains(document.activeElement)) return
      focusHandoff = label
      queueMicrotask(() => {
        focusHandoff = null
      })
    }
  }, [label])

  // When the list scrolls sideways (a phone), keep the selected tab inside it: a link to a later
  // tab would otherwise show a tab bar with no visible selection. Only the list scrolls, never the
  // page, so opening a page does not move the reader.
  useLayoutEffect(() => {
    const list = listRef.current
    const selected = list?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')
    if (!list || !selected) return
    const listBox = list.getBoundingClientRect()
    const tabBox = selected.getBoundingClientRect()
    if (tabBox.left < listBox.left) list.scrollLeft -= listBox.left - tabBox.left
    else if (tabBox.right > listBox.right) list.scrollLeft += tabBox.right - listBox.right
  }, [value])

  return (
    <Tabs.Root value={value} onValueChange={(details) => onChange(details.value)}>
      {/* The label goes on the tablist itself, the element assistive technology announces. */}
      <Tabs.List
        ref={listRef}
        aria-label={label}
        maxW="full"
        overflowX="auto"
        overscrollBehaviorX="contain"
        scrollbarWidth="none"
        css={{ '&::-webkit-scrollbar': { display: 'none' } }}
      >
        {items.map((item, index) => {
          const Icon = item.icon
          return (
            <Tabs.Trigger
              key={item.value}
              value={item.value}
              flex="none"
              // Chakra keeps only the selected tab focusable; with none selected, keep the first one
              // reachable so keyboard users can still leave a Not Found route through the tabs.
              tabIndex={selectsNothing && index === 0 ? 0 : undefined}
            >
              {Icon && <Icon size={16} aria-hidden />}
              {item.label}
            </Tabs.Trigger>
          )
        })}
      </Tabs.List>
    </Tabs.Root>
  )
}
