import { useMemo, useState } from 'react'
import {
  Button,
  Dropdown,
  DropdownItem,
  DropdownGroup,
  DropdownList,
  MenuToggle,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  TextInput,
} from '@patternfly/react-core'
import { BookmarkIcon, PencilAltIcon, PlusIcon, TrashIcon } from '@patternfly/react-icons'
import { EMPTY_SERVER_FILTERS, type ServerFilters, type ServerGroupBy, type ServerSortKey, type SortDirection } from '@/domain/server/list'
import { useToast } from '@/presentation/components/toast/toastContext'

const STORAGE_KEY = 'swallow.servers.saved-views'

/** Table row density stored in each local saved view. */
export type ServerDensity = 'compact' | 'comfortable'

/**
 * Complete reusable working-set view. Site is intentionally excluded because URL scope
 * must remain authoritative when a view is applied in another Site.
 */
export interface SavedServerViewState {
  filters: ServerFilters
  keyword: string
  includeAbsent: boolean
  groupBy: ServerGroupBy
  sortKey: ServerSortKey
  sortDirection: SortDirection
  hiddenColumns: string[]
  density: ServerDensity
  pageSize: number
}

interface SavedServerView { id: string; name: string; state: SavedServerViewState }

function loadViews(): SavedServerView[] {
  try {
    const parsed = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '[]') as SavedServerView[]
    if (!Array.isArray(parsed)) return []
    return parsed.map((view) => ({
      ...view,
      state: {
        ...view.state,
        filters: {
          ...EMPTY_SERVER_FILTERS,
          ...view.state.filters,
          lockState: view.state.filters?.lockState ?? 'any',
        },
      },
    }))
  } catch { return [] }
}

function saveViews(views: SavedServerView[]): void {
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(views)) } catch {
    // A blocked storage API degrades persistence, not the mounted working set.
  }
}

/** Local NetBox-style saved views with case-insensitive uniqueness and no Site scope. */
export function ServerSavedViews({ current, onApply }: { current: SavedServerViewState; onApply: (state: SavedServerViewState) => void }) {
  const { showToast } = useToast()
  const [views, setViews] = useState<SavedServerView[]>(loadViews)
  const [open, setOpen] = useState(false)
  const [modalOpen, setModalOpen] = useState(false)
  const [editingId, setEditingId] = useState<string>()
  const [name, setName] = useState('')
  const editing = useMemo(() => views.find((view) => view.id === editingId), [editingId, views])

  const openCreate = () => { setEditingId(undefined); setName(''); setModalOpen(true); setOpen(false) }
  const openRename = (view: SavedServerView) => { setEditingId(view.id); setName(view.name); setModalOpen(true); setOpen(false) }
  const commit = () => {
    const trimmed = name.trim()
    if (!trimmed) return
    if (views.some((view) => view.id !== editingId && view.name.toLocaleLowerCase() === trimmed.toLocaleLowerCase())) {
      showToast({ title: 'View name already exists', description: 'Saved view names are case-insensitive.', tone: 'warning' })
      return
    }
    const next = editing ? views.map((view) => view.id === editing.id ? { ...view, name: trimmed } : view) : [...views, { id: crypto.randomUUID(), name: trimmed, state: current }]
    setViews(next); saveViews(next); setModalOpen(false)
    showToast({ title: editing ? 'View renamed' : 'View saved', tone: 'success' })
  }
  const remove = (id: string) => { const next = views.filter((view) => view.id !== id); setViews(next); saveViews(next) }

  return <>
    <Dropdown isOpen={open} onOpenChange={setOpen} toggle={(ref) => <MenuToggle ref={ref} icon={<BookmarkIcon />} isExpanded={open} onClick={() => setOpen((value) => !value)}>Saved views</MenuToggle>}>
      <DropdownList>
        {views.length === 0 && <DropdownItem isDisabled>No saved views</DropdownItem>}
        {views.map((view) => <DropdownGroup key={view.id} label={view.name}><DropdownItem icon={<BookmarkIcon />} onClick={() => { onApply(view.state); setOpen(false) }}>Apply</DropdownItem><DropdownItem icon={<PencilAltIcon />} aria-label={`Rename ${view.name}`} onClick={() => openRename(view)}>Rename</DropdownItem><DropdownItem icon={<TrashIcon />} aria-label={`Delete ${view.name}`} onClick={() => { remove(view.id); setOpen(false) }}>Delete</DropdownItem></DropdownGroup>)}
        <DropdownItem icon={<PlusIcon />} onClick={openCreate}>Save current view</DropdownItem>
      </DropdownList>
    </Dropdown>
    <Modal isOpen={modalOpen} onClose={() => setModalOpen(false)} variant="small" aria-labelledby="saved-view-title">
      <ModalHeader title={editing ? 'Rename saved view' : 'Save current view'} labelId="saved-view-title" description="Stores filters, grouping, sorting, columns, density, and page size in this browser." />
      <ModalBody><label htmlFor="saved-view-name"><strong>Name</strong></label><TextInput id="saved-view-name" value={name} onChange={(_event, value) => setName(value)} autoFocus onKeyDown={(event) => { if (event.key === 'Enter') commit() }} /></ModalBody>
      <ModalFooter><Button onClick={commit} isDisabled={!name.trim()}>{editing ? 'Rename' : 'Save view'}</Button><Button variant="link" onClick={() => setModalOpen(false)}>Cancel</Button></ModalFooter>
    </Modal>
  </>
}
