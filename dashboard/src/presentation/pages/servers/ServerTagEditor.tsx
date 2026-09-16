import { useEffect, useMemo, useState } from 'react'
import { Badge, Box, Button, Field, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { Plus } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/toast/toastContext'
import type { ServerTagOption, ServerTagsResult } from '@/domain/provisioning/types'
import type { Server } from '@/domain/server/types'
import { serverDisplayName } from '@/domain/server/list'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import {
  buildTagRows,
  computeTagDiff,
  cycleTagDecision,
  tagCheckboxState,
  type TagDecision,
} from './serverTagEditing'

interface ServerTagEditorProps {
  /** The Servers to edit; one for a single edit, many for a batch edit. Never empty. */
  servers: readonly Server[]
  onClose: () => void
  /** Called with each Server's effective tags after a successful save, so the caller can reload. */
  onSaved: (results: ServerTagsResult[]) => void
}

/** Tag names accept letters, digits, and `_-.` — mirroring the backend rule so the UI rejects early. */
const TAG_NAME_PATTERN = /^[A-Za-z0-9_.-]+$/
const MAX_TAG_NAME_LENGTH = 100

/**
 * Shared tri-state tag editor for one or many Servers, used by both the batch action on the servers
 * list and the single-Server action on the detail page.
 *
 * Each tag shows its state across the selection like an email label: checked when every selected
 * Server has it, unchecked when none does, and indeterminate when some do. Toggling a tag applies or
 * removes it across all selected Servers, and only the tags whose state actually changed are sent as
 * a diff (decision 031). Automatic tags (provider-computed, e.g. one that may back `amd-gpu`) are
 * shown disabled because swallow cannot assign them. Ownership — provider-driven or swallow-owned —
 * is resolved by the backend; this dialog only expresses intent.
 */
export function ServerTagEditor({ servers, onClose, onSaved }: ServerTagEditorProps) {
  const { provisioning } = useApp()
  const { showToast } = useToast()
  // Known tags come from the first Server's Site. A selection almost always shares one Site; when it
  // does not, this is a best-effort suggestion list and the edit still applies per Server on save.
  const siteId = servers[0]?.source.siteId ?? ''
  const [known, setKnown] = useState<ServerTagOption[] | null>(null)
  const [knownWarning, setKnownWarning] = useState('')
  const [extraTags, setExtraTags] = useState<string[]>([])
  const [decisions, setDecisions] = useState<Map<string, TagDecision>>(new Map())
  const [newTagInput, setNewTagInput] = useState('')
  const [inputError, setInputError] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    let cancelled = false
    setKnown(null)
    setKnownWarning('')
    provisioning
      .listServerTags(siteId)
      .then((tags) => {
        if (!cancelled) setKnown(tags)
      })
      .catch((caught: unknown) => {
        // A failed suggestion list must not block editing: fall back to only the tags already on the
        // selected Servers and warn, rather than trapping the operator in an error dialog.
        if (cancelled) return
        setKnown([])
        setKnownWarning(caught instanceof Error ? caught.message : 'Existing tags could not be loaded.')
      })
    return () => {
      cancelled = true
    }
  }, [provisioning, siteId])

  // Rows are the union of known tags, freshly typed tags, and tags already on the selection. Extra
  // tags are offered as editable because the operator just created them.
  const rows = useMemo(() => {
    const options: ServerTagOption[] = [...(known ?? []), ...extraTags.map((name) => ({ name, editable: true }))]
    return buildTagRows(servers, options)
  }, [known, extraTags, servers])

  const diff = useMemo(() => computeTagDiff(rows, decisions), [rows, decisions])
  const hasChanges = diff.add.length > 0 || diff.remove.length > 0
  const total = servers.length

  const toggle = (name: string) => {
    const row = rows.find((candidate) => candidate.name === name)
    if (!row || !row.editable) return
    setDecisions((previous) => {
      const next = new Map(previous)
      const decision = cycleTagDecision(row.coverage, previous.get(name))
      if (decision === undefined) next.delete(name)
      else next.set(name, decision)
      return next
    })
  }

  const addNewTag = () => {
    const name = newTagInput.trim()
    if (name === '') return
    if (name.length > MAX_TAG_NAME_LENGTH || !TAG_NAME_PATTERN.test(name)) {
      setInputError('Use letters, digits, and _-. only.')
      return
    }
    setInputError('')
    setNewTagInput('')
    // Applying an existing tag is the same as checking it; a brand-new name becomes an extra row.
    const existing = rows.find((row) => row.name === name)
    if (existing && !existing.editable) {
      setInputError('That tag is managed by the provisioner and cannot be assigned.')
      return
    }
    if (!existing) setExtraTags((previous) => (previous.includes(name) ? previous : [...previous, name]))
    setDecisions((previous) => new Map(previous).set(name, 'add'))
  }

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (!hasChanges || submitting) return
    setSubmitting(true)
    setError('')
    try {
      const results = await provisioning.editServerTags({
        serverIds: servers.map((server) => server.id),
        add: diff.add,
        remove: diff.remove,
      })
      showToast({
        tone: 'success',
        title: 'Tags updated',
        description:
          total === 1 ? `Updated tags on ${serverDisplayName(servers[0])}.` : `Updated tags on ${total} servers.`,
      })
      onSaved(results)
      onClose()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Tags could not be saved.')
    } finally {
      setSubmitting(false)
    }
  }

  const description =
    total === 1
      ? `Edit the tags on ${serverDisplayName(servers[0])}.`
      : `Edit tags across ${total} selected servers. A tag shown as mixed is on only some of them.`

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title="Edit tags"
      description={description}
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={!hasChanges || submitting}>
            Save tags
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Tags could not be saved">
            {error}
          </Alert>
        )}
        {knownWarning && (
          <Alert status="warning" title="Existing tags could not be loaded">
            You can still edit tags already on the selected servers and add new ones. {knownWarning}
          </Alert>
        )}
        {known === null ? (
          <Text color="fg.muted">Loading tags…</Text>
        ) : (
          <>
            {rows.length === 0 ? (
              <Text color="fg.muted">No tags yet. Add one below.</Text>
            ) : (
              <Stack gap="1" role="group" aria-label="Tags">
                {rows.map((row) => (
                  <HStack key={row.name} justify="space-between" gap="3">
                    <Checkbox
                      id={`tag-${row.name}`}
                      checked={tagCheckboxState(row.coverage, decisions.get(row.name))}
                      disabled={!row.editable}
                      onCheckedChange={() => toggle(row.name)}
                    >
                      {row.name}
                    </Checkbox>
                    <TagRowHint editable={row.editable} coverage={row.coverage} count={row.count} total={total} />
                  </HStack>
                ))}
              </Stack>
            )}
            <Field.Root invalid={Boolean(inputError)}>
              <Field.Label>Add a tag</Field.Label>
              <HStack gap="2" align="flex-start">
                <Input
                  value={newTagInput}
                  placeholder="e.g. rack-a"
                  aria-label="New tag name"
                  onChange={(event) => setNewTagInput(event.target.value)}
                  onKeyDown={(event) => {
                    // Enter adds the tag rather than submitting the whole form, so a half-typed name
                    // is not saved by accident.
                    if (event.key === 'Enter') {
                      event.preventDefault()
                      addNewTag()
                    }
                  }}
                />
                <Button variant="outline" onClick={addNewTag} disabled={newTagInput.trim() === ''}>
                  <Plus size={16} />
                  Add
                </Button>
              </HStack>
              {inputError && <Field.ErrorText>{inputError}</Field.ErrorText>}
            </Field.Root>
          </>
        )}
      </Stack>
    </Modal>
  )
}

/**
 * Right-aligned hint for a tag row: a non-colour status of the tag's coverage across the selection,
 * plus a disabled note for provider-managed automatic tags. Status is conveyed by text, not colour.
 */
function TagRowHint({
  editable,
  coverage,
  count,
  total,
}: {
  editable: boolean
  coverage: 'all' | 'some' | 'none'
  count: number
  total: number
}) {
  if (!editable) {
    return (
      <Tooltip content="Automatic tag, managed by the provisioner. It cannot be assigned from swallow.">
        <Badge variant="subtle" colorPalette="gray">
          Automatic
        </Badge>
      </Tooltip>
    )
  }
  if (total > 1 && coverage === 'some') {
    return (
      <Box as="span" fontSize="xs" color="fg.muted">
        on {count} of {total}
      </Box>
    )
  }
  return null
}
