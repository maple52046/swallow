import { useMemo, useState } from 'react'
import { Checkbox, Flex, ScrollArea, Text, TextField } from '@radix-ui/themes'
import { MagnifyingGlassIcon } from '@radix-ui/react-icons'

/** One selectable option. `count` is an optional occurrence badge (e.g. how many servers). */
export interface FilterOption {
  value: string
  label: string
  count?: number
}

interface CheckboxFilterListProps {
  options: FilterOption[]
  /** Currently checked values. */
  selected: readonly string[]
  onToggle: (value: string) => void
  /** Show an in-list search box; defaults on when there are more than eight options. */
  searchable?: boolean
  emptyText?: string
}

/**
 * A searchable checkbox list for one filter dimension (status, zone, pool, tags, …).
 *
 * This is the reusable "searchable select" the filter panel composes once per dimension,
 * so every dimension filters and searches identically. Selection is controlled by the
 * caller; this component owns only the local search text. Each option is a real
 * `<label>` wrapping a checkbox, so it is clickable and announced with its state.
 */
export function CheckboxFilterList({
  options,
  selected,
  onToggle,
  searchable,
  emptyText = 'No options',
}: CheckboxFilterListProps) {
  const [query, setQuery] = useState('')
  const showSearch = searchable ?? options.length > 8

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase()
    if (!needle) return options
    return options.filter((option) => option.label.toLowerCase().includes(needle))
  }, [options, query])

  const selectedSet = useMemo(() => new Set(selected), [selected])

  return (
    <Flex direction="column" gap="2">
      {showSearch && (
        <TextField.Root
          size="1"
          placeholder="Filter options"
          value={query}
          onChange={(event) => setQuery(event.currentTarget.value)}
        >
          <TextField.Slot>
            <MagnifyingGlassIcon />
          </TextField.Slot>
        </TextField.Root>
      )}

      <ScrollArea type="auto" style={{ maxHeight: 180 }}>
        <Flex direction="column" gap="1" pr="2">
          {visible.length === 0 ? (
            <Text size="1" color="gray">
              {emptyText}
            </Text>
          ) : (
            visible.map((option) => (
              <Text key={option.value} as="label" size="2">
                <Flex align="center" gap="2" justify="between">
                  <Flex align="center" gap="2">
                    <Checkbox
                      checked={selectedSet.has(option.value)}
                      onCheckedChange={() => onToggle(option.value)}
                    />
                    {option.label}
                  </Flex>
                  {option.count !== undefined && (
                    <Text size="1" color="gray">
                      {option.count}
                    </Text>
                  )}
                </Flex>
              </Text>
            ))
          )}
        </Flex>
      </ScrollArea>
    </Flex>
  )
}
