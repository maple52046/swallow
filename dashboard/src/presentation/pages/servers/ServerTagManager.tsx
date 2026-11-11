import { useEffect, useMemo, useState } from 'react'
import { Badge, Box, Button, Field, HStack, IconButton, Input, Stack, Text } from '@chakra-ui/react'
import { Pencil, Trash2 } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import type { ServerTagOption } from '@/domain/provisioning/types'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { LoadingState } from '@/presentation/components/LoadingState'
import { failureOf, failureText, type DisplayedFailure } from '@/presentation/components/failure'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { Tooltip } from '@/presentation/components/ui/tooltip'

/** A Site the tag manager can act on: one with an enabled provisioner in the current scope. */
export interface TagManagerSite {
  id: string
  name: string
}

interface ServerTagManagerProps {
  /** The Sites whose tags can be managed; never empty. The first is selected initially. */
  sites: readonly TagManagerSite[]
  onClose: () => void
  /**
   * Called after a deletion or rename succeeds, so the servers list reloads: Server specs changed,
   * and the provisioner changes the machines' tags shortly after.
   */
  onChanged: () => void
}

/** Tag names accept letters, digits, and `_-.` — mirroring the backend rule so the UI rejects early. */
const TAG_NAME_PATTERN = /^[A-Za-z0-9_.-]+$/
const MAX_TAG_NAME_LENGTH = 100

/** The Site's tag list: loading, failed (with the request ID), or loaded. */
type TagListState =
  | { status: 'loading' }
  | { status: 'error'; failure: DisplayedFailure }
  | { status: 'ready'; tags: ServerTagOption[] }

/** The inline rename in progress: the tag being renamed, the typed name, and its validation error. */
interface RenameDraft {
  tag: ServerTagOption
  value: string
  error: string
}

/** One tag a multi-tag deletion could not delete, with the reason the API gave. */
interface TagDeletionFailure {
  name: string
  failure: DisplayedFailure
}

/**
 * Site-wide tag management for the servers list (server-tags.md): every tag the Site knows, how many
 * Servers carry it, and whether the provisioner manages it, with rename and delete.
 *
 * Data comes from the provisioning port's tag list for the selected Site and is reloaded after each
 * change; the per-Server tag editor stays the way to apply or remove a tag on chosen Servers.
 *
 * - Deleting a manual tag removes it from every Server of the Site that carries it, then from the
 *   provisioner. Deleting an automatic tag deletes it in the provisioner only, so the confirmation
 *   says what happens next: one the provisioner maintains may come back, one computed from a rule
 *   takes its rule with it. Every deletion is confirmed in an `alertdialog` naming the tag, the
 *   number of Servers, and the consequence.
 * - Several tags can be selected (or all at once) and deleted together. The confirmation lists each
 *   tag with its consequence; the tags are then deleted one after another, because two deletions
 *   in parallel would rewrite the same Server specs and the later write would fail on the
 *   generation the earlier one changed. A tag that fails does not stop the others: it stays
 *   selected and the dialog lists it with the API's reason, so the user can retry it.
 * - Renaming is offered for manual tags only. A new name applies at once (it is reversible); a name
 *   that already exists merges the two tags and is confirmed first, because it cannot be split again.
 * - Loading, failure (with retry and request ID), empty, and action failures are distinct states;
 *   tag kinds are text badges, never colour alone, and every icon button has an accessible name.
 */
export function ServerTagManager({ sites, onClose, onChanged }: ServerTagManagerProps) {
  const { provisioning } = useApp()
  const { showToast } = useToast()
  const [siteId, setSiteId] = useState(sites[0]?.id ?? '')
  const [list, setList] = useState<TagListState>({ status: 'loading' })
  const [reloadKey, setReloadKey] = useState(0)
  const [rename, setRename] = useState<RenameDraft | null>(null)
  const [pendingMerge, setPendingMerge] = useState<{ from: string; to: string } | null>(null)
  const [pendingDelete, setPendingDelete] = useState<ServerTagOption | null>(null)
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const [pendingBulkDelete, setPendingBulkDelete] = useState(false)
  // How many of the selected tags the running multi-tag deletion has finished, for its progress text.
  const [bulkProgress, setBulkProgress] = useState(0)
  const [bulkFailures, setBulkFailures] = useState<TagDeletionFailure[]>([])
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState<DisplayedFailure | null>(null)
  const siteName = sites.find((site) => site.id === siteId)?.name ?? siteId

  // Loads the selected Site's tags. A later Site change or reload ignores this response, so a slow
  // read never overwrites the list of the Site now shown.
  useEffect(() => {
    let cancelled = false
    setList({ status: 'loading' })
    provisioning
      .listServerTags(siteId)
      .then((tags) => {
        if (!cancelled) setList({ status: 'ready', tags })
      })
      .catch((caught: unknown) => {
        if (!cancelled) setList({ status: 'error', failure: failureOf(caught, 'The tags could not be loaded.') })
      })
    return () => {
      cancelled = true
    }
  }, [provisioning, siteId, reloadKey])

  const tags = useMemo(
    () => (list.status === 'ready' ? [...list.tags].sort((a, b) => a.name.localeCompare(b.name)) : []),
    [list],
  )
  // The selection as the listed tags, in list order; a selected name the reloaded list no longer
  // holds simply drops out.
  const selectedTags = useMemo(() => tags.filter((tag) => selected.has(tag.name)), [tags, selected])
  const selectAllState: boolean | 'indeterminate' =
    selectedTags.length === 0 ? false : selectedTags.length === tags.length ? true : 'indeterminate'

  const unselect = (name: string) => {
    setSelected((previous) => {
      const next = new Set(previous)
      next.delete(name)
      return next
    })
  }

  const changed = (title: string, description: string) => {
    showToast({ tone: 'success', title, description })
    setRename(null)
    setPendingMerge(null)
    setPendingDelete(null)
    setBulkFailures([])
    setReloadKey((key) => key + 1)
    onChanged()
  }

  const runRename = async (from: string, to: string) => {
    setBusy(true)
    setActionError(null)
    try {
      const servers = await provisioning.renameServerTag(siteId, from, to)
      unselect(from)
      changed('Tag renamed', `Renamed ${from} to ${to} on ${serverCountText(servers.length)}.`)
    } catch (caught) {
      setPendingMerge(null)
      setActionError(failureOf(caught, 'The tag could not be renamed.'))
    } finally {
      setBusy(false)
    }
  }

  const submitRename = () => {
    if (!rename || busy) return
    const to = rename.value.trim()
    if (to === rename.tag.name) {
      setRename(null)
      return
    }
    if (to === '' || to.length > MAX_TAG_NAME_LENGTH || !TAG_NAME_PATTERN.test(to)) {
      setRename({ ...rename, error: 'Use letters, digits, and _-. only.' })
      return
    }
    const existing = tags.find((tag) => tag.name === to)
    if (existing && !existing.editable) {
      setRename({ ...rename, error: 'That name belongs to an automatic tag managed by the provisioner.' })
      return
    }
    if (existing) {
      setPendingMerge({ from: rename.tag.name, to })
      return
    }
    void runRename(rename.tag.name, to)
  }

  const runDelete = async (tag: ServerTagOption) => {
    setBusy(true)
    setActionError(null)
    try {
      await provisioning.deleteServerTag(siteId, tag.name)
      unselect(tag.name)
      changed('Tag deleted', `Deleted ${tag.name} from ${siteName}.`)
    } catch (caught) {
      setPendingDelete(null)
      setActionError(failureOf(caught, 'The tag could not be deleted.'))
    } finally {
      setBusy(false)
    }
  }

  const runBulkDelete = async (targets: readonly ServerTagOption[]) => {
    setBusy(true)
    setActionError(null)
    setBulkFailures([])
    setBulkProgress(0)
    const failures: TagDeletionFailure[] = []
    // One at a time: two deletions in parallel rewrite the same Servers' specs, and the later
    // write would fail on the generation the earlier one changed.
    for (const tag of targets) {
      try {
        await provisioning.deleteServerTag(siteId, tag.name)
      } catch (caught) {
        failures.push({ name: tag.name, failure: failureOf(caught, 'The tag could not be deleted.') })
      }
      setBulkProgress((done) => done + 1)
    }
    const deleted = targets.length - failures.length
    setBusy(false)
    setPendingBulkDelete(false)
    setSelected(new Set(failures.map((failure) => failure.name)))
    setBulkFailures(failures)
    if (deleted === 0) return
    showToast(
      failures.length === 0
        ? { tone: 'success', title: 'Tags deleted', description: `Deleted ${tagCountText(deleted)} from ${siteName}.` }
        : {
            tone: 'warning',
            title: 'Some tags were not deleted',
            description: `Deleted ${deleted} of ${tagCountText(targets.length)}; the dialog lists the rest.`,
          },
    )
    setReloadKey((key) => key + 1)
    onChanged()
  }

  const mergeTarget = pendingMerge ? tags.find((tag) => tag.name === pendingMerge.to) : undefined
  const mergeSource = pendingMerge ? tags.find((tag) => tag.name === pendingMerge.from) : undefined

  return (
    <>
      <Modal
        open
        onClose={() => !busy && onClose()}
        closeOnInteractOutside={!busy}
        size="lg"
        title="Manage tags"
        description={`Tags known for ${siteName}. Deleting or renaming a tag changes every Server of the Site that carries it.`}
        // The multi-tag deletion is the dialog's one footer action, after Close; it appears with a
        // tag list and stays disabled until a tag is selected.
        footer={
          <>
            <Button variant="ghost" onClick={onClose} disabled={busy}>
              Close
            </Button>
            {list.status === 'ready' && tags.length > 0 && (
              <Button
                colorPalette="red"
                disabled={busy || selectedTags.length === 0}
                onClick={() => {
                  setActionError(null)
                  setPendingBulkDelete(true)
                }}
              >
                <Trash2 size={16} />
                {selectedTags.length === 0 ? 'Delete selected' : `Delete ${tagCountText(selectedTags.length)}`}
              </Button>
            )}
          </>
        }
      >
        <Stack gap="4">
          {sites.length > 1 && (
            <Field.Root>
              <Field.Label htmlFor="tag-manager-site">Site</Field.Label>
              <Select
                id="tag-manager-site"
                aria-label="Site"
                value={siteId}
                disabled={busy}
                onChange={(value) => {
                  setRename(null)
                  setActionError(null)
                  setBulkFailures([])
                  setSelected(new Set())
                  setSiteId(value)
                }}
                options={sites.map((site) => ({ value: site.id, label: site.name }))}
              />
            </Field.Root>
          )}
          {actionError && (
            <Alert status="error" title="The change was not applied">
              <FailureMessage failure={actionError} />
            </Alert>
          )}
          {bulkFailures.length > 0 && (
            <Alert
              status="error"
              title={`${tagCountText(bulkFailures.length)} ${bulkFailures.length === 1 ? 'was' : 'were'} not deleted`}
            >
              <Stack gap="2">
                <Box as="ul" ps="5" listStyleType="disc">
                  {bulkFailures.map((item) => (
                    <li key={item.name}>
                      {item.name}: {failureText(item.failure)}
                    </li>
                  ))}
                </Box>
                <Text>They stay selected, so you can delete them again.</Text>
              </Stack>
            </Alert>
          )}
          {list.status === 'loading' && <LoadingState rows={4} height={36} />}
          {list.status === 'error' && (
            <ErrorState
              message={list.failure.message}
              requestId={list.failure.requestId}
              onRetry={() => setReloadKey((key) => key + 1)}
            />
          )}
          {list.status === 'ready' && tags.length === 0 && (
            <EmptyState
              title="No tags yet"
              message="Add tags to Servers with Edit tags; they appear here once the Site knows them."
            />
          )}
          {list.status === 'ready' && tags.length > 0 && (
            <Checkbox
              checked={selectAllState}
              disabled={busy}
              onCheckedChange={(checked) => setSelected(checked ? new Set(tags.map((tag) => tag.name)) : new Set())}
            >
              Select all tags
            </Checkbox>
          )}
          {list.status === 'ready' && tags.length > 0 && (
            <Stack as="ul" gap="0" role="list" aria-label={`Tags of ${siteName}`} listStyleType="none">
              {tags.map((tag) => (
                <Box
                  as="li"
                  key={tag.name}
                  py="2"
                  borderBottomWidth="1px"
                  borderColor="border.muted"
                  _last={{ borderBottomWidth: 0 }}
                >
                  {rename?.tag.name === tag.name ? (
                    <RenameRow
                      draft={rename}
                      busy={busy}
                      onChange={(value) => setRename({ ...rename, value, error: '' })}
                      onSubmit={submitRename}
                      onCancel={() => setRename(null)}
                    />
                  ) : (
                    <ManagedTagRow
                      tag={tag}
                      busy={busy}
                      selected={selected.has(tag.name)}
                      onSelectedChange={(checked) =>
                        setSelected((previous) => {
                          const next = new Set(previous)
                          if (checked) next.add(tag.name)
                          else next.delete(tag.name)
                          return next
                        })
                      }
                      onRename={() => {
                        setActionError(null)
                        setRename({ tag, value: tag.name, error: '' })
                      }}
                      onDelete={() => {
                        setActionError(null)
                        setPendingDelete(tag)
                      }}
                    />
                  )}
                </Box>
              ))}
            </Stack>
          )}
        </Stack>
      </Modal>
      <ConfirmDialog
        open={pendingDelete !== null}
        title={pendingDelete ? `Delete tag ${pendingDelete.name}?` : ''}
        confirmLabel="Delete tag"
        busy={busy}
        onCancel={() => setPendingDelete(null)}
        onConfirm={() => pendingDelete && void runDelete(pendingDelete)}
      >
        {pendingDelete && <Text>{deletionConsequence(pendingDelete)}</Text>}
      </ConfirmDialog>
      <ConfirmDialog
        open={pendingBulkDelete}
        title={`Delete ${tagCountText(selectedTags.length)}?`}
        confirmLabel={`Delete ${tagCountText(selectedTags.length)}`}
        busy={busy}
        onCancel={() => setPendingBulkDelete(false)}
        onConfirm={() => void runBulkDelete(selectedTags)}
      >
        <Stack gap="3">
          <Text>Each tag is deleted from {siteName} and its provisioner, one after another:</Text>
          {/* Focusable so a keyboard user can scroll a long selection. */}
          <Box as="ul" ps="5" listStyleType="disc" maxH="60" overflowY="auto" tabIndex={0} aria-label="Tags to delete">
            {selectedTags.map((tag) => (
              <li key={tag.name}>
                {tag.name}: {bulkDeletionConsequence(tag)}
              </li>
            ))}
          </Box>
          <Text>Tags can select monitoring and automation targets, so the Servers that carried them may leave them.</Text>
          {busy && (
            <Text role="status" color="fg.muted">
              Deleted {bulkProgress} of {tagCountText(selectedTags.length)}…
            </Text>
          )}
        </Stack>
      </ConfirmDialog>
      <ConfirmDialog
        open={pendingMerge !== null}
        title={pendingMerge ? `Merge ${pendingMerge.from} into ${pendingMerge.to}?` : ''}
        confirmLabel="Merge tags"
        busy={busy}
        onCancel={() => setPendingMerge(null)}
        onConfirm={() => pendingMerge && void runRename(pendingMerge.from, pendingMerge.to)}
      >
        {pendingMerge && (
          <Text>
            {pendingMerge.to} already exists on {serverCountText(mergeTarget?.serverCount ?? 0)}. The{' '}
            {serverCountText(mergeSource?.serverCount ?? 0)} with {pendingMerge.from} get {pendingMerge.to} instead, and{' '}
            {pendingMerge.from} is deleted from the provisioner. The two tags cannot be told apart afterwards.
          </Text>
        )}
      </ConfirmDialog>
    </>
  )
}

/**
 * One tag in the manager's list: a checkbox labelled with its name for multi-tag deletion, how many
 * Servers carry it, its kind as a text badge with a tooltip explaining what swallow can do with it,
 * and the actions its kind allows. An automatic tag has no rename, because the provisioner names it.
 */
function ManagedTagRow({
  tag,
  busy,
  selected,
  onSelectedChange,
  onRename,
  onDelete,
}: {
  tag: ServerTagOption
  busy: boolean
  selected: boolean
  onSelectedChange: (selected: boolean) => void
  onRename: () => void
  onDelete: () => void
}) {
  // The name side takes the remaining width and wraps inside it, so a long tag name never pushes
  // the actions onto their own line on a phone.
  return (
    <HStack justify="space-between" align="flex-start" gap="3">
      <HStack gap="2" flex="1" minW="0" wrap="wrap" pt="1.5">
        <Checkbox checked={selected} disabled={busy} onCheckedChange={onSelectedChange}>
          <Text as="span" fontWeight="medium" overflowWrap="anywhere">
            {tag.name}
          </Text>
        </Checkbox>
        {!tag.editable && (
          <Tooltip content={automaticTagExplanation(tag)}>
            <Badge variant="subtle" colorPalette="gray">
              Automatic
            </Badge>
          </Tooltip>
        )}
        <Text fontSize="sm" color="fg.muted">
          {tag.serverCount === 0 ? 'Unused' : serverCountText(tag.serverCount)}
        </Text>
      </HStack>
      <HStack gap="1" flexShrink="0">
        {tag.editable && (
          <IconButton
            aria-label={`Rename tag ${tag.name}`}
            variant="ghost"
            size="sm"
            disabled={busy}
            onClick={onRename}
          >
            <Pencil size={16} />
          </IconButton>
        )}
        <IconButton
          aria-label={`Delete tag ${tag.name}`}
          variant="ghost"
          size="sm"
          colorPalette="red"
          disabled={busy}
          onClick={onDelete}
        >
          <Trash2 size={16} />
        </IconButton>
      </HStack>
    </HStack>
  )
}

/**
 * The inline rename of one manual tag. Enter saves (instead of submitting anything else) and Cancel
 * leaves the rename without closing the manager, so a keyboard user can rename tag after tag; the
 * field takes focus when it opens and announces a validation error through its error text.
 */
function RenameRow({
  draft,
  busy,
  onChange,
  onSubmit,
  onCancel,
}: {
  draft: RenameDraft
  busy: boolean
  onChange: (value: string) => void
  onSubmit: () => void
  onCancel: () => void
}) {
  return (
    <Field.Root invalid={Boolean(draft.error)}>
      <Field.Label>New name for {draft.tag.name}</Field.Label>
      <HStack gap="2" align="flex-start" wrap="wrap">
        <Input
          flex="1"
          minW="40"
          autoFocus
          value={draft.value}
          disabled={busy}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              onSubmit()
            }
          }}
        />
        <Button colorPalette="brand" size="sm" onClick={onSubmit} loading={busy} disabled={busy || draft.value.trim() === ''}>
          Save
        </Button>
        <Button variant="ghost" size="sm" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
      </HStack>
      {draft.error && <Field.ErrorText>{draft.error}</Field.ErrorText>}
    </Field.Root>
  )
}

/** "1 server" or "N servers". */
function serverCountText(count: number): string {
  return count === 1 ? '1 server' : `${count} servers`
}

/** "1 tag" or "N tags". */
function tagCountText(count: number): string {
  return count === 1 ? '1 tag' : `${count} tags`
}

/**
 * The short consequence of deleting one tag in a multi-tag deletion, by kind, so the confirmation
 * can list every selected tag on one line each; the full sentences are `deletionConsequence`'s.
 */
function bulkDeletionConsequence(tag: ServerTagOption): string {
  const carriers = serverCountText(tag.serverCount)
  if (tag.editable) return tag.serverCount === 0 ? 'unused' : `removed from ${carriers}`
  if (tag.removable) return `automatic; taken off ${carriers} now, the provisioner may add it again`
  return `automatic rule; deleted with its rule, it leaves ${carriers}`
}

/** What swallow can do with an automatic tag, for its badge tooltip. */
function automaticTagExplanation(tag: ServerTagOption): string {
  return tag.removable
    ? 'Automatic tag, maintained by the provisioner. You can remove or delete it, but the provisioner may add it again.'
    : 'Automatic tag, computed by the provisioner from a rule. Deleting it deletes the rule too.'
}

/**
 * The consequence statement of deleting a tag, by kind: what leaves which Servers, and whether the
 * tag can come back. Tags select monitoring and automation targets, so the statement says so.
 */
function deletionConsequence(tag: ServerTagOption): string {
  const carriers = serverCountText(tag.serverCount)
  if (tag.editable) {
    return tag.serverCount === 0
      ? `No Server carries ${tag.name}; it is deleted from the provisioner. You can add it again later.`
      : `${tag.name} is removed from ${carriers} and deleted from the provisioner. Tags can select monitoring and automation targets, so these Servers may leave them. You can add the tag again later.`
  }
  if (tag.removable) {
    return `${tag.name} is maintained by the provisioner. Deleting it takes it off ${carriers} now, but the provisioner may add it again, for example when a machine is inspected.`
  }
  return `${tag.name} is computed by the provisioner from a rule. Deleting it deletes the rule, so it leaves ${carriers} and does not come back. Server Type and monitoring targets that read it change with it.`
}
