import type { ReactNode } from 'react'
import { List, Stack, Text } from '@chakra-ui/react'
import type { ActiveOperationSummary } from '@/presentation/hooks/useServerActiveOperations'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'

interface BlockingWorkflowsNoticeProps {
  /** The unfinished Workflows holding the action's Servers; nothing renders when empty. */
  operations: readonly ActiveOperationSummary[]
  title: string
  /** What happens when the box is checked, in the action's own words. */
  children: ReactNode
  checkboxId: string
  checkboxLabel: string
  /** Accessible name of the list of Workflows. */
  listLabel: string
  checked: boolean
  disabled?: boolean
  onCheckedChange: (checked: boolean) => void
}

/**
 * The confirmation every Server-holding action shares before it asks the backend to cancel the
 * Workflows in its way (`cancelActiveWorkflows`, decision 061): it names each Workflow and its
 * status as text, and the operator opts in with a checkbox. The dialog sends the flag and closes;
 * the backend does the canceling and starts the action once those Workflows end.
 */
export function BlockingWorkflowsNotice({
  operations,
  title,
  children,
  checkboxId,
  checkboxLabel,
  listLabel,
  checked,
  disabled,
  onCheckedChange,
}: BlockingWorkflowsNoticeProps) {
  if (operations.length === 0) return null
  return (
    <Alert status="warning" title={title}>
      <Stack gap="2">
        <Text>{children}</Text>
        <List.Root aria-label={listLabel}>
          {operations.map((operation) => (
            <List.Item key={operation.id}>
              {operation.intent} — {operation.status}
            </List.Item>
          ))}
        </List.Root>
        <Checkbox id={checkboxId} checked={checked} disabled={disabled} onCheckedChange={onCheckedChange}>
          {checkboxLabel}
        </Checkbox>
      </Stack>
    </Alert>
  )
}
