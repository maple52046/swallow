import { useState } from 'react'
import { Button, Field, Input, List, Stack, Text } from '@chakra-ui/react'
import { deleteServers } from '@/application/usecases/servers/deleteServers'
import { serverDisplayName } from '@/domain/server/list'
import type { Server } from '@/domain/server/types'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import {
  persistServerActionResult,
  rejectedServerActionOutcome,
  serverActionRunResult,
  type ServerActionRunResult,
} from './serverActionResults'
import { useServerActiveOperations } from './useServerActiveOperations'

interface ServerDeleteDialogProps {
  /** Servers to delete. One Server is confirmed by typing its name, several by typing the count. */
  targets: readonly Server[]
  onClose: () => void
  /**
   * Called once the deletions settled and at least one succeeded, with the run result so the page
   * can show per-Server failures. The parent owns navigation or list refresh.
   */
  onDeleted: (result: ServerActionRunResult) => void
}

/** How many target names the dialog lists before summarizing the rest. */
const LISTED_TARGETS = 10

/** Joins display names for a sentence, summarizing past the listed count. */
function namesSentence(servers: readonly Server[]): string {
  const names = servers.slice(0, LISTED_TARGETS).map(serverDisplayName)
  const remaining = servers.length - names.length
  return remaining > 0 ? `${names.join(', ')} and ${remaining} more` : names.join(', ')
}

/**
 * Confirms the provider-backed deletion shared by Server list rows, the list's bulk selection,
 * and the detail page.
 *
 * Deleting removes the provisioner's Machine and then the Swallow record, one Server at a time
 * through the per-Server API with at most four in flight; provider safeguards can refuse any of
 * them. A single Server is confirmed by typing its displayed name, and a refusal stays inline. A
 * bulk selection lists its targets and is confirmed by typing how many Servers will be deleted;
 * while it runs the dialog reports how many have finished, so a long batch never looks frozen.
 * When every deletion failed it stays open with the first reason, otherwise it hands the result
 * to the page. Targets that belong to a Platform or that unfinished Workflows still hold are
 * named in warnings but not excluded, because deletion does not cancel that work or uninstall the
 * Platform.
 */
export function ServerDeleteDialog({ targets, onClose, onDeleted }: ServerDeleteDialogProps) {
  const { servers } = useApp()
  const { showToast } = useToast()
  const { activeOperations } = useServerActiveOperations(targets.map((target) => target.id))
  const [confirmation, setConfirmation] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [settled, setSettled] = useState(0)

  const single = targets.length === 1 ? targets[0] : undefined
  const count = targets.length
  const expected = single ? serverDisplayName(single) : String(count)
  const title = single ? 'Delete server' : `Delete ${count} servers`
  const platformMembers = targets.filter((server) => server.membership?.platformId)
  const heldIds = new Set(activeOperations.flatMap((operation) => operation.serverIds))
  const held = targets.filter((server) => heldIds.has(server.id))

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (confirmation !== expected || submitting || count === 0) return
    setSubmitting(true)
    setSettled(0)
    setError('')
    try {
      const outcomes = await deleteServers(servers, targets.map((target) => target.id), setSettled)
      const result = serverActionRunResult('delete', outcomes.map((outcome, index) => {
        const target = { serverId: targets[index].id, serverName: serverDisplayName(targets[index]) }
        return outcome.error === undefined ? { ...target, accepted: true } : rejectedServerActionOutcome(target, outcome.error)
      }))
      persistServerActionResult(result)
      const failures = result.outcomes.filter((outcome) => !outcome.accepted)
      if (result.succeeded === 0) {
        setError(single
          ? failures[0]?.message ?? 'Could not delete the Server.'
          : `None of the ${count} servers could be deleted. ${failures[0]?.serverName}: ${failures[0]?.message ?? 'Unknown error'}`)
        return
      }
      showToast(failures.length === 0
        ? {
            tone: 'success',
            title: single ? 'Server deleted' : `${result.succeeded} servers deleted`,
            description: single
              ? `${expected} was removed from the provisioner and Swallow.`
              : 'They were removed from the provisioner and Swallow.',
          }
        : {
            tone: 'warning',
            title: 'Some servers were not deleted',
            description: `${result.succeeded} deleted; ${failures.length} refused. Review the result details.`,
          })
      onDeleted(result)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      role="alertdialog"
      closeOnInteractOutside={!submitting}
      title={title}
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="red" loading={submitting} disabled={confirmation !== expected || submitting || count === 0}>
            {title}
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title={single ? 'Server could not be deleted' : 'Servers could not be deleted'}>
            {error}
          </Alert>
        )}
        <Alert status="warning" title="This cannot be undone">
          Swallow asks the provisioner to permanently delete {single ? 'the Machine' : 'each Machine'}, then removes its
          Server record. Provider safeguards are not overridden; if a deletion is refused, neither record of that Server
          is removed.
        </Alert>
        {!single && (
          <Field.Root>
            <Field.Label>{`${count} servers will be deleted`}</Field.Label>
            <List.Root aria-label="Servers to delete" listStyle="none" ps="0">
              {targets.slice(0, LISTED_TARGETS).map((server) => (
                <List.Item key={server.id}>{serverDisplayName(server)}</List.Item>
              ))}
            </List.Root>
            {count > LISTED_TARGETS && <Text color="fg.muted">and {count - LISTED_TARGETS} more</Text>}
          </Field.Root>
        )}
        {platformMembers.length > 0 && (
          <Alert
            status="warning"
            title={single
              ? 'This server belongs to a Platform'
              : `${platformMembers.length} of these servers ${platformMembers.length === 1 ? 'belongs' : 'belong'} to a Platform`}
          >
            {namesSentence(platformMembers)}. Deleting does not uninstall the Platform; review its membership afterwards.
          </Alert>
        )}
        {held.length > 0 && (
          <Alert
            status="warning"
            title={single
              ? 'This server has running operations'
              : `${held.length} of these servers ${held.length === 1 ? 'has' : 'have'} running operations`}
          >
            {namesSentence(held)}. The running operations are not canceled and can fail once their server is gone.
          </Alert>
        )}
        {submitting && !single && (
          <Text aria-live="polite">{`Deleted ${settled} of ${count}…`}</Text>
        )}
        <Field.Root required>
          <Field.Label>
            {single ? `Type "${expected}" to confirm` : `Type ${count} to confirm`} <Field.RequiredIndicator />
          </Field.Label>
          <Input
            value={confirmation}
            onChange={(event) => setConfirmation(event.target.value)}
            autoFocus
            inputMode={single ? undefined : 'numeric'}
            aria-label={single ? 'Server name confirmation' : 'Server count confirmation'}
          />
        </Field.Root>
      </Stack>
    </Modal>
  )
}
