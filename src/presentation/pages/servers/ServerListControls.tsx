import { Box, Button, DropdownMenu, Flex, Popover, Select, Text, TextField } from '@radix-ui/themes'
import { Cross2Icon, MagnifyingGlassIcon, MixerHorizontalIcon } from '@radix-ui/react-icons'
import { Accordion, type AccordionSection } from '@/presentation/components/radix/Accordion'
import { CheckboxFilterList, type FilterOption } from '@/presentation/components/radix/CheckboxFilterList'
import {
  EMPTY_SERVER_FILTERS,
  isEmptyFilters,
  type ServerFilters,
  type ServerGroupBy,
} from '@/domain/server/list'
import { SERVER_ACTION_GROUPS, type BulkAction } from './serverActions'

/** Distinct option lists for each filter dimension, with occurrence counts. */
export interface ServerFilterOptions {
  provisioningState: FilterOption[]
  zone: FilterOption[]
  pool: FilterOption[]
  tag: FilterOption[]
}

/** A toggleable column, minus the always-on name column. */
export interface ColumnToggle {
  key: string
  label: string
}

interface ServerListControlsProps {
  keyword: string
  onKeyword: (value: string) => void
  includeAbsent: boolean
  onIncludeAbsent: (value: boolean) => void
  groupBy: ServerGroupBy
  onGroupBy: (value: ServerGroupBy) => void
  filters: ServerFilters
  onFilters: (filters: ServerFilters) => void
  filterOptions: ServerFilterOptions
  columns: ColumnToggle[]
  hiddenColumns: ReadonlySet<string>
  onToggleColumn: (key: string) => void
  pageSize: number
  onPageSize: (size: number) => void
  selectedCount: number
  onClearSelection: () => void
  onBulkAction: (action: BulkAction) => void
  bulkRunning: boolean
}

const GROUP_OPTIONS: { value: ServerGroupBy; label: string }[] = [
  { value: 'none', label: 'No grouping' },
  { value: 'provisioning', label: 'Group by status' },
  { value: 'zone', label: 'Group by zone' },
  { value: 'pool', label: 'Group by resource pool' },
  { value: 'architecture', label: 'Group by architecture' },
  { value: 'power', label: 'Group by power' },
]

const PAGE_SIZE_OPTIONS = [50, 100, 200]

/**
 * The servers list toolbar.
 *
 * With no rows selected it shows search, grouping, a multi-dimension filter popover, a
 * column show/hide menu, and page size — mirroring MAAS's `MachineListControls`. When rows
 * are selected it swaps to a bulk-action bar (matching MAAS, which replaces the toolbar on
 * selection), offering the full action catalogue; the backend refuses any a given server
 * cannot do. All controls are presentational: state is owned by the page.
 */
export function ServerListControls(props: ServerListControlsProps) {
  if (props.selectedCount > 0) {
    return (
      <Flex align="center" justify="between" gap="3" wrap="wrap">
        <Text size="2" weight="medium">
          {props.selectedCount} selected
        </Text>
        <Flex align="center" gap="2">
          <DropdownMenu.Root>
            <DropdownMenu.Trigger>
              <Button variant="solid" loading={props.bulkRunning}>
                Actions
                <DropdownMenu.TriggerIcon />
              </Button>
            </DropdownMenu.Trigger>
            <DropdownMenu.Content>
              {SERVER_ACTION_GROUPS.map((group, groupIndex) => (
                <DropdownMenu.Group key={group.label}>
                  {groupIndex > 0 && <DropdownMenu.Separator />}
                  <DropdownMenu.Label>{group.label}</DropdownMenu.Label>
                  {group.actions.map((entry) => (
                    <DropdownMenu.Item
                      key={entry.action}
                      color={entry.destructive ? 'red' : undefined}
                      onSelect={() => props.onBulkAction(entry.action)}
                    >
                      {entry.label}
                    </DropdownMenu.Item>
                  ))}
                </DropdownMenu.Group>
              ))}
            </DropdownMenu.Content>
          </DropdownMenu.Root>
          <Button variant="soft" color="gray" onClick={props.onClearSelection}>
            <Cross2Icon />
            Clear selection
          </Button>
        </Flex>
      </Flex>
    )
  }

  const filterSections: AccordionSection[] = [
    {
      value: 'status',
      label: 'Status',
      count: props.filters.provisioningStates.length || undefined,
      content: (
        <CheckboxFilterList
          options={props.filterOptions.provisioningState}
          selected={props.filters.provisioningStates}
          onToggle={(value) =>
            props.onFilters(toggleDimension(props.filters, 'provisioningStates', value))
          }
        />
      ),
    },
    {
      value: 'zone',
      label: 'Zone',
      count: props.filters.zones.length || undefined,
      content: (
        <CheckboxFilterList
          options={props.filterOptions.zone}
          selected={props.filters.zones}
          onToggle={(value) => props.onFilters(toggleDimension(props.filters, 'zones', value))}
        />
      ),
    },
    {
      value: 'pool',
      label: 'Resource pool',
      count: props.filters.pools.length || undefined,
      content: (
        <CheckboxFilterList
          options={props.filterOptions.pool}
          selected={props.filters.pools}
          onToggle={(value) => props.onFilters(toggleDimension(props.filters, 'pools', value))}
        />
      ),
    },
    {
      value: 'tags',
      label: 'Tags',
      count: props.filters.tags.length || undefined,
      content: (
        <CheckboxFilterList
          options={props.filterOptions.tag}
          selected={props.filters.tags}
          onToggle={(value) => props.onFilters(toggleDimension(props.filters, 'tags', value))}
        />
      ),
    },
  ]

  const activeFilterCount =
    props.filters.provisioningStates.length +
    props.filters.zones.length +
    props.filters.pools.length +
    props.filters.tags.length +
    (props.filters.hasGpu !== null ? 1 : 0)

  return (
    <Flex align="center" gap="2" wrap="wrap">
      <Box style={{ flex: 1, minWidth: 220 }}>
        <TextField.Root
          placeholder="hostname, FQDN, or address"
          value={props.keyword}
          onChange={(event) => props.onKeyword(event.currentTarget.value)}
        >
          <TextField.Slot>
            <MagnifyingGlassIcon />
          </TextField.Slot>
        </TextField.Root>
      </Box>

      <Popover.Root>
        <Popover.Trigger>
          <Button variant="soft" color={activeFilterCount > 0 ? undefined : 'gray'}>
            <MixerHorizontalIcon />
            Filters{activeFilterCount > 0 ? ` (${activeFilterCount})` : ''}
          </Button>
        </Popover.Trigger>
        <Popover.Content style={{ width: 300 }}>
          <Flex direction="column" gap="2">
            <Accordion sections={filterSections} defaultOpen={['status']} />
            <Text as="label" size="2">
              <Flex align="center" gap="2">
                <input
                  type="checkbox"
                  checked={props.filters.hasGpu === true}
                  onChange={(event) =>
                    props.onFilters({
                      ...props.filters,
                      hasGpu: event.currentTarget.checked ? true : null,
                    })
                  }
                />
                Has GPU
              </Flex>
            </Text>
            {!isEmptyFilters(props.filters) && (
              <Button
                variant="soft"
                color="gray"
                size="1"
                onClick={() => props.onFilters(EMPTY_SERVER_FILTERS)}
              >
                Clear filters
              </Button>
            )}
          </Flex>
        </Popover.Content>
      </Popover.Root>

      <Select.Root value={props.groupBy} onValueChange={(value) => props.onGroupBy(value as ServerGroupBy)}>
        <Select.Trigger />
        <Select.Content>
          {GROUP_OPTIONS.map((option) => (
            <Select.Item key={option.value} value={option.value}>
              {option.label}
            </Select.Item>
          ))}
        </Select.Content>
      </Select.Root>

      <DropdownMenu.Root>
        <DropdownMenu.Trigger>
          <Button variant="soft" color="gray">
            Columns
            <DropdownMenu.TriggerIcon />
          </Button>
        </DropdownMenu.Trigger>
        <DropdownMenu.Content>
          {props.columns.map((column) => (
            <DropdownMenu.CheckboxItem
              key={column.key}
              checked={!props.hiddenColumns.has(column.key)}
              onSelect={(event) => {
                // Keep the menu open while toggling several columns.
                event.preventDefault()
                props.onToggleColumn(column.key)
              }}
            >
              {column.label}
            </DropdownMenu.CheckboxItem>
          ))}
        </DropdownMenu.Content>
      </DropdownMenu.Root>

      <Select.Root
        value={String(props.pageSize)}
        onValueChange={(value) => props.onPageSize(Number(value))}
      >
        <Select.Trigger />
        <Select.Content>
          {PAGE_SIZE_OPTIONS.map((size) => (
            <Select.Item key={size} value={String(size)}>
              {size} / page
            </Select.Item>
          ))}
        </Select.Content>
      </Select.Root>

      <Text as="label" size="2">
        <Flex align="center" gap="2">
          <input
            type="checkbox"
            checked={props.includeAbsent}
            onChange={(event) => props.onIncludeAbsent(event.currentTarget.checked)}
          />
          Include absent
        </Flex>
      </Text>
    </Flex>
  )
}

/** Adds or removes a value from one array-valued filter dimension, returning a new filter. */
function toggleDimension(
  filters: ServerFilters,
  dimension: 'provisioningStates' | 'zones' | 'pools' | 'tags',
  value: string,
): ServerFilters {
  const current = filters[dimension]
  const next = current.includes(value)
    ? current.filter((entry) => entry !== value)
    : [...current, value]
  return { ...filters, [dimension]: next }
}
