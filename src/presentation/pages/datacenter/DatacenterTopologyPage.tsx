import { useCallback, useEffect, useState } from 'react'
import {
  ActionIcon, Badge, Button, Card, Divider, Group, Modal,
  NumberInput, ScrollArea, Stack, Text, TextInput, Textarea,
  ThemeIcon, Tooltip, Collapse, UnstyledButton,
} from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { notifications } from '@mantine/notifications'
import {
  IconBuilding, IconChevronDown, IconChevronRight, IconDoor,
  IconEdit, IconLayersIntersect, IconPlus, IconRefresh, IconServer,
  IconTrash,
} from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { Datacenter, Room, Rack, CreateDatacenterInput, UpdateDatacenterInput, CreateRoomInput, UpdateRoomInput, CreateRackInput, UpdateRackInput } from '@/domain/topology/types'
import { PageHeader } from '@/presentation/components/PageHeader'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { formatRelative } from '@/shared/utils/time'

type SelectedType = 'dc' | 'room' | 'rack'
type Selection = { type: SelectedType; id: string } | null

// ─── Datacenter Form Modal ──────────────────────────────────────────────────

function DatacenterFormModal({
  opened, onClose, initial, onSaved,
}: {
  opened: boolean
  onClose: () => void
  initial?: Datacenter
  onSaved: (dc: Datacenter) => void
}) {
  const { topology } = useApp()
  const [name, setName] = useState(initial?.name ?? '')
  const [code, setCode] = useState(initial?.code ?? '')
  const [location, setLocation] = useState(initial?.location ?? '')
  const [description, setDescription] = useState(initial?.description ?? '')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (opened) {
      setName(initial?.name ?? '')
      setCode(initial?.code ?? '')
      setLocation(initial?.location ?? '')
      setDescription(initial?.description ?? '')
    }
  }, [opened, initial])

  const handleSubmit = async () => {
    if (!name.trim()) return
    try {
      setSubmitting(true)
      let saved: Datacenter
      if (initial) {
        const input: UpdateDatacenterInput = { name: name.trim(), code: code.trim() || undefined, location: location.trim() || undefined, description: description.trim() || undefined }
        saved = await topology.datacenters.update.execute(initial.id, input)
      } else {
        const input: CreateDatacenterInput = { name: name.trim(), code: code.trim() || undefined, location: location.trim() || undefined, description: description.trim() || undefined }
        saved = await topology.datacenters.create.execute(input)
      }
      notifications.show({ title: initial ? 'Datacenter updated' : 'Datacenter created', message: saved.name, color: 'green' })
      onSaved(saved)
      onClose()
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal opened={opened} onClose={onClose} title={initial ? 'Edit Datacenter' : 'New Datacenter'} centered withinPortal zIndex={2000}>
      <Stack gap="md">
        <TextInput label="Name" placeholder="e.g. DC-East" value={name} onChange={(e) => setName(e.target.value)} required />
        <TextInput label="Code" placeholder="e.g. DCE" value={code} onChange={(e) => setCode(e.target.value)} />
        <TextInput label="Location" placeholder="e.g. Virginia, US" value={location} onChange={(e) => setLocation(e.target.value)} />
        <Textarea label="Description" placeholder="Optional notes" value={description} onChange={(e) => setDescription(e.target.value)} minRows={2} />
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>Cancel</Button>
          <Button onClick={() => void handleSubmit()} loading={submitting} disabled={!name.trim()}>{initial ? 'Save' : 'Create'}</Button>
        </Group>
      </Stack>
    </Modal>
  )
}

// ─── Room Form Modal ─────────────────────────────────────────────────────────

function RoomFormModal({
  opened, onClose, initial, datacenterId, onSaved,
}: {
  opened: boolean
  onClose: () => void
  initial?: Room
  datacenterId: string
  onSaved: (room: Room) => void
}) {
  const { topology } = useApp()
  const [name, setName] = useState(initial?.name ?? '')
  const [floor, setFloor] = useState(initial?.floor ?? '')
  const [description, setDescription] = useState(initial?.description ?? '')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (opened) {
      setName(initial?.name ?? '')
      setFloor(initial?.floor ?? '')
      setDescription(initial?.description ?? '')
    }
  }, [opened, initial])

  const handleSubmit = async () => {
    if (!name.trim()) return
    try {
      setSubmitting(true)
      let saved: Room
      if (initial) {
        const input: UpdateRoomInput = { name: name.trim(), floor: floor.trim() || undefined, description: description.trim() || undefined }
        saved = await topology.rooms.update.execute(initial.id, input)
      } else {
        const input: CreateRoomInput = { datacenterId, name: name.trim(), floor: floor.trim() || undefined, description: description.trim() || undefined }
        saved = await topology.rooms.create.execute(input)
      }
      notifications.show({ title: initial ? 'Room updated' : 'Room created', message: saved.name, color: 'green' })
      onSaved(saved)
      onClose()
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal opened={opened} onClose={onClose} title={initial ? 'Edit Room' : 'New Room'} centered withinPortal zIndex={2000}>
      <Stack gap="md">
        <TextInput label="Room Name" placeholder="e.g. Room-A" value={name} onChange={(e) => setName(e.target.value)} required />
        <TextInput label="Floor" placeholder="e.g. 1F" value={floor} onChange={(e) => setFloor(e.target.value)} />
        <Textarea label="Description" placeholder="Optional notes" value={description} onChange={(e) => setDescription(e.target.value)} minRows={2} />
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>Cancel</Button>
          <Button onClick={() => void handleSubmit()} loading={submitting} disabled={!name.trim()}>{initial ? 'Save' : 'Create'}</Button>
        </Group>
      </Stack>
    </Modal>
  )
}

// ─── Rack Form Modal ─────────────────────────────────────────────────────────

function RackFormModal({
  opened, onClose, initial, datacenterId, roomId, onSaved,
}: {
  opened: boolean
  onClose: () => void
  initial?: Rack
  datacenterId: string
  roomId: string | null
  onSaved: (rack: Rack) => void
}) {
  const { topology } = useApp()
  const [name, setName] = useState(initial?.name ?? '')
  const [rackUnit, setRackUnit] = useState<number | string>(initial?.rackUnit ?? '')
  const [description, setDescription] = useState(initial?.description ?? '')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (opened) {
      setName(initial?.name ?? '')
      setRackUnit(initial?.rackUnit ?? '')
      setDescription(initial?.description ?? '')
    }
  }, [opened, initial])

  const handleSubmit = async () => {
    if (!name.trim()) return
    try {
      setSubmitting(true)
      const ruNum = rackUnit !== '' ? Number(rackUnit) : undefined
      let saved: Rack
      if (initial) {
        const input: UpdateRackInput = { name: name.trim(), rackUnit: ruNum, description: description.trim() || undefined }
        saved = await topology.racks.update.execute(initial.id, input)
      } else {
        const input: CreateRackInput = { datacenterId, roomId, name: name.trim(), rackUnit: ruNum, description: description.trim() || undefined }
        saved = await topology.racks.create.execute(input)
      }
      notifications.show({ title: initial ? 'Rack updated' : 'Rack created', message: saved.name, color: 'green' })
      onSaved(saved)
      onClose()
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal opened={opened} onClose={onClose} title={initial ? 'Edit Rack' : 'New Rack'} centered withinPortal zIndex={2000}>
      <Stack gap="md">
        <TextInput label="Rack Name" placeholder="e.g. R01" value={name} onChange={(e) => setName(e.target.value)} required />
        <NumberInput label="Rack Units (U)" placeholder="e.g. 42" value={rackUnit} onChange={setRackUnit} min={1} max={100} />
        <Textarea label="Description" placeholder="Optional notes" value={description} onChange={(e) => setDescription(e.target.value)} minRows={2} />
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>Cancel</Button>
          <Button onClick={() => void handleSubmit()} loading={submitting} disabled={!name.trim()}>{initial ? 'Save' : 'Create'}</Button>
        </Group>
      </Stack>
    </Modal>
  )
}

// ─── Info Row ────────────────────────────────────────────────────────────────

function InfoRow({ label, value }: { label: string; value?: string | number | null }) {
  return (
    <Group justify="space-between" py={4} style={{ borderBottom: '1px solid var(--mantine-color-default-border)' }}>
      <Text size="sm" c="dimmed">{label}</Text>
      <Text size="sm" fw={500}>{value ?? <Text component="span" c="dimmed" size="sm">—</Text>}</Text>
    </Group>
  )
}

// ─── Topology Tree Node ──────────────────────────────────────────────────────

function TreeNode({
  label, icon, depth = 0, active, onClick, children, badge, defaultOpen = true,
}: {
  label: string
  icon: React.ReactNode
  depth?: number
  active?: boolean
  onClick: () => void
  children?: React.ReactNode
  badge?: React.ReactNode
  defaultOpen?: boolean
}) {
  const [open, setOpen] = useState(defaultOpen)
  const hasChildren = !!children

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', paddingLeft: depth * 16 + 8 }}>
        {hasChildren ? (
          <UnstyledButton
            onClick={(e) => { e.stopPropagation(); setOpen((o) => !o) }}
            style={{ display: 'flex', alignItems: 'center', padding: '4px 2px' }}
          >
            <ThemeIcon size="xs" variant="transparent" c="dimmed">
              {open ? <IconChevronDown size={11} /> : <IconChevronRight size={11} />}
            </ThemeIcon>
          </UnstyledButton>
        ) : (
          <div style={{ width: 20 }} />
        )}
        <UnstyledButton
          flex={1}
          onClick={onClick}
          py={5}
          px="xs"
          style={{
            display: 'flex', alignItems: 'center', gap: 6, borderRadius: 6,
            background: active ? 'var(--mantine-color-blue-light)' : undefined,
          }}
        >
          <ThemeIcon size="xs" variant="transparent" c={active ? 'blue' : 'dimmed'}>{icon}</ThemeIcon>
          <Text size="sm" flex={1} c={active ? 'blue' : undefined} fw={active ? 600 : 400} truncate>{label}</Text>
          {badge}
        </UnstyledButton>
      </div>
      {hasChildren && (
        <Collapse in={open}>
          {children}
        </Collapse>
      )}
    </div>
  )
}

// ─── Detail Panel ─────────────────────────────────────────────────────────────

interface DetailPanelProps {
  selection: Selection
  dcs: Datacenter[]
  rooms: Room[]
  racks: Rack[]
  onEdit: () => void
  onDelete: () => void
  onCreateRoom: () => void
  onCreateRack: () => void
}

function DetailPanel({ selection, dcs, rooms, racks, onEdit, onDelete, onCreateRoom, onCreateRack }: DetailPanelProps) {
  if (!selection) {
    return (
      <Stack align="center" justify="center" h="100%" c="dimmed" gap="xs">
        <IconBuilding size={40} opacity={0.3} />
        <Text size="sm">Select an item from the tree to view details</Text>
      </Stack>
    )
  }

  if (selection.type === 'dc') {
    const dc = dcs.find((d) => d.id === selection.id)
    if (!dc) return null
    const dcRooms = rooms.filter((r) => r.datacenterId === dc.id)
    const dcRacks = racks.filter((r) => r.datacenterId === dc.id)
    const noRoomRacks = dcRacks.filter((r) => !r.roomId)
    return (
      <Stack gap="lg">
        <Group justify="space-between" align="flex-start">
          <Group gap="sm">
            <ThemeIcon color="blue" variant="light" size="lg"><IconBuilding size={18} /></ThemeIcon>
            <Stack gap={0}>
              <Text fw={700} size="lg">{dc.name}</Text>
              <Badge size="xs" variant="outline" color="blue">Datacenter</Badge>
            </Stack>
          </Group>
          <Group gap="xs">
            <Tooltip label="Add Room"><ActionIcon variant="light" size="sm" onClick={onCreateRoom}><IconPlus size={14} /></ActionIcon></Tooltip>
            <Tooltip label="Add Rack (no room)"><ActionIcon variant="light" size="sm" color="teal" onClick={onCreateRack}><IconLayersIntersect size={14} /></ActionIcon></Tooltip>
            <Tooltip label="Edit"><ActionIcon variant="light" color="gray" size="sm" onClick={onEdit}><IconEdit size={14} /></ActionIcon></Tooltip>
            <Tooltip label="Delete"><ActionIcon variant="light" color="red" size="sm" onClick={onDelete}><IconTrash size={14} /></ActionIcon></Tooltip>
          </Group>
        </Group>
        <Stack gap={2}>
          <InfoRow label="Code" value={dc.code} />
          <InfoRow label="Location" value={dc.location} />
          <InfoRow label="Description" value={dc.description} />
          <InfoRow label="Created" value={formatRelative(dc.createdAt)} />
          <InfoRow label="Updated" value={formatRelative(dc.updatedAt)} />
        </Stack>
        <Group gap="md">
          <Card withBorder p="sm" style={{ flex: 1, textAlign: 'center' }}>
            <Text size="xl" fw={700} c="blue">{dcRooms.length}</Text>
            <Text size="xs" c="dimmed">Rooms</Text>
          </Card>
          <Card withBorder p="sm" style={{ flex: 1, textAlign: 'center' }}>
            <Text size="xl" fw={700} c="teal">{dcRacks.length}</Text>
            <Text size="xs" c="dimmed">Total Racks</Text>
          </Card>
          <Card withBorder p="sm" style={{ flex: 1, textAlign: 'center' }}>
            <Text size="xl" fw={700} c="gray">{noRoomRacks.length}</Text>
            <Text size="xs" c="dimmed">Racks (no room)</Text>
          </Card>
        </Group>
      </Stack>
    )
  }

  if (selection.type === 'room') {
    const room = rooms.find((r) => r.id === selection.id)
    if (!room) return null
    const dc = dcs.find((d) => d.id === room.datacenterId)
    const roomRacks = racks.filter((r) => r.roomId === room.id)
    return (
      <Stack gap="lg">
        <Group justify="space-between" align="flex-start">
          <Group gap="sm">
            <ThemeIcon color="violet" variant="light" size="lg"><IconDoor size={18} /></ThemeIcon>
            <Stack gap={0}>
              <Text fw={700} size="lg">{room.name}</Text>
              <Badge size="xs" variant="outline" color="violet">Room</Badge>
            </Stack>
          </Group>
          <Group gap="xs">
            <Tooltip label="Add Rack"><ActionIcon variant="light" size="sm" onClick={onCreateRack}><IconPlus size={14} /></ActionIcon></Tooltip>
            <Tooltip label="Edit"><ActionIcon variant="light" color="gray" size="sm" onClick={onEdit}><IconEdit size={14} /></ActionIcon></Tooltip>
            <Tooltip label="Delete"><ActionIcon variant="light" color="red" size="sm" onClick={onDelete}><IconTrash size={14} /></ActionIcon></Tooltip>
          </Group>
        </Group>
        <Stack gap={2}>
          <InfoRow label="Datacenter" value={dc?.name} />
          <InfoRow label="Floor" value={room.floor} />
          <InfoRow label="Description" value={room.description} />
          <InfoRow label="Created" value={formatRelative(room.createdAt)} />
          <InfoRow label="Updated" value={formatRelative(room.updatedAt)} />
        </Stack>
        <Card withBorder p="sm" style={{ textAlign: 'center', maxWidth: 160 }}>
          <Text size="xl" fw={700} c="teal">{roomRacks.length}</Text>
          <Text size="xs" c="dimmed">Racks</Text>
        </Card>
      </Stack>
    )
  }

  if (selection.type === 'rack') {
    const rack = racks.find((r) => r.id === selection.id)
    if (!rack) return null
    const dc = dcs.find((d) => d.id === rack.datacenterId)
    const room = rooms.find((r) => r.id === rack.roomId)
    return (
      <Stack gap="lg">
        <Group justify="space-between" align="flex-start">
          <Group gap="sm">
            <ThemeIcon color="teal" variant="light" size="lg"><IconLayersIntersect size={18} /></ThemeIcon>
            <Stack gap={0}>
              <Text fw={700} size="lg">{rack.name}</Text>
              <Badge size="xs" variant="outline" color="teal">Rack</Badge>
            </Stack>
          </Group>
          <Group gap="xs">
            <Tooltip label="Edit"><ActionIcon variant="light" color="gray" size="sm" onClick={onEdit}><IconEdit size={14} /></ActionIcon></Tooltip>
            <Tooltip label="Delete"><ActionIcon variant="light" color="red" size="sm" onClick={onDelete}><IconTrash size={14} /></ActionIcon></Tooltip>
          </Group>
        </Group>
        <Stack gap={2}>
          <InfoRow label="Datacenter" value={dc?.name} />
          <InfoRow label="Room" value={room?.name ?? '(none)'} />
          <InfoRow label="Rack Units" value={rack.rackUnit ? `${rack.rackUnit}U` : undefined} />
          <InfoRow label="Description" value={rack.description} />
          <InfoRow label="Created" value={formatRelative(rack.createdAt)} />
          <InfoRow label="Updated" value={formatRelative(rack.updatedAt)} />
        </Stack>
        <Card withBorder p="sm" style={{ background: 'var(--mantine-color-default-hover)' }}>
          <Group gap="xs">
            <IconServer size={14} />
            <Text size="sm" c="dimmed">Server assignment visible in Server List</Text>
          </Group>
        </Card>
      </Stack>
    )
  }

  return null
}

// ─── Main Page ───────────────────────────────────────────────────────────────

type ModalKind =
  | { type: 'create-dc' }
  | { type: 'edit-dc'; dc: Datacenter }
  | { type: 'create-room'; datacenterId: string }
  | { type: 'edit-room'; room: Room }
  | { type: 'create-rack'; datacenterId: string; roomId: string | null }
  | { type: 'edit-rack'; rack: Rack }
  | null

export function DatacenterTopologyPage() {
  const { topology } = useApp()

  const [dcs, setDcs] = useState<Datacenter[]>([])
  const [rooms, setRooms] = useState<Room[]>([])
  const [racks, setRacks] = useState<Rack[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [selection, setSelection] = useState<Selection>(null)
  const [activeModal, setActiveModal] = useState<ModalKind>(null)
  const [deleteOpened, { open: openDelete, close: closeDelete }] = useDisclosure(false)
  const [deleting, setDeleting] = useState(false)

  const load = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const [dcList, roomList, rackList] = await Promise.all([
        topology.datacenters.list.execute(),
        topology.rooms.list.execute(),
        topology.racks.list.execute(),
      ])
      setDcs(dcList)
      setRooms(roomList)
      setRacks(rackList)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [topology.datacenters.list, topology.rooms.list, topology.racks.list])

  useEffect(() => { void load() }, [load])

  const handleDelete = async () => {
    if (!selection) return
    try {
      setDeleting(true)
      if (selection.type === 'dc') {
        await topology.datacenters.delete.execute(selection.id)
        setDcs((prev) => prev.filter((d) => d.id !== selection.id))
      } else if (selection.type === 'room') {
        await topology.rooms.delete.execute(selection.id)
        setRooms((prev) => prev.filter((r) => r.id !== selection.id))
      } else if (selection.type === 'rack') {
        await topology.racks.delete.execute(selection.id)
        setRacks((prev) => prev.filter((r) => r.id !== selection.id))
      }
      notifications.show({ title: 'Deleted', message: 'Item removed successfully', color: 'gray' })
      setSelection(null)
      closeDelete()
    } catch (e) {
      notifications.show({ title: 'Cannot delete', message: String(e), color: 'red' })
      closeDelete()
    } finally {
      setDeleting(false)
    }
  }

  const getDeleteLabel = () => {
    if (!selection) return ''
    if (selection.type === 'dc') return dcs.find((d) => d.id === selection.id)?.name ?? ''
    if (selection.type === 'room') return rooms.find((r) => r.id === selection.id)?.name ?? ''
    if (selection.type === 'rack') return racks.find((r) => r.id === selection.id)?.name ?? ''
    return ''
  }

  const selectedDc = selection?.type === 'dc' ? dcs.find((d) => d.id === selection.id) : undefined
  const selectedRoom = selection?.type === 'room' ? rooms.find((r) => r.id === selection.id) : undefined
  const selectedRack = selection?.type === 'rack' ? racks.find((r) => r.id === selection.id) : undefined

  const openEditModal = () => {
    if (!selection) return
    if (selection.type === 'dc' && selectedDc) setActiveModal({ type: 'edit-dc', dc: selectedDc })
    else if (selection.type === 'room' && selectedRoom) setActiveModal({ type: 'edit-room', room: selectedRoom })
    else if (selection.type === 'rack' && selectedRack) setActiveModal({ type: 'edit-rack', rack: selectedRack })
  }

  const openCreateRoom = () => {
    const dcId = selection?.type === 'dc' ? selection.id
      : selection?.type === 'room' ? selectedRoom?.datacenterId ?? ''
      : ''
    if (dcId) setActiveModal({ type: 'create-room', datacenterId: dcId })
  }

  const openCreateRack = () => {
    if (selection?.type === 'dc') setActiveModal({ type: 'create-rack', datacenterId: selection.id, roomId: null })
    else if (selection?.type === 'room' && selectedRoom) setActiveModal({ type: 'create-rack', datacenterId: selectedRoom.datacenterId, roomId: selectedRoom.id })
    else if (selection?.type === 'rack' && selectedRack) setActiveModal({ type: 'create-rack', datacenterId: selectedRack.datacenterId, roomId: selectedRack.roomId })
  }

  if (loading) return <LoadingState rows={6} />
  if (error) return <ErrorState message={error} onRetry={() => void load()} />

  return (
    <>
      <PageHeader
        title="Datacenter"
        subtitle="Manage datacenter topology — datacenters, rooms, and racks"
        actions={
          <Group gap="sm">
            <ActionIcon variant="default" onClick={() => void load()}><IconRefresh size={16} /></ActionIcon>
            <Button size="sm" leftSection={<IconPlus size={14} />} onClick={() => setActiveModal({ type: 'create-dc' })}>
              New Datacenter
            </Button>
          </Group>
        }
      />

      <Group align="flex-start" gap={0} style={{ height: 'calc(100vh - 160px)', minHeight: 500 }}>
        {/* Left tree */}
        <div style={{ width: 300, borderRight: '1px solid var(--mantine-color-default-border)', height: '100%' }}>
          <ScrollArea h="100%" p="xs">
            {dcs.length === 0 ? (
              <EmptyState message="No datacenters yet." />
            ) : (
              <Stack gap={2}>
                {dcs.map((dc) => {
                  const dcRooms = rooms.filter((r) => r.datacenterId === dc.id)
                  const noRoomRacks = racks.filter((r) => r.datacenterId === dc.id && !r.roomId)
                  const isActiveDc = selection?.type === 'dc' && selection.id === dc.id
                  return (
                    <TreeNode
                      key={dc.id}
                      label={dc.name}
                      icon={<IconBuilding size={13} />}
                      active={isActiveDc}
                      onClick={() => setSelection({ type: 'dc', id: dc.id })}
                      badge={
                        <Badge size="xs" variant="light" color="blue">
                          {dcRooms.length}R / {racks.filter((r) => r.datacenterId === dc.id).length}K
                        </Badge>
                      }
                    >
                      {dcRooms.map((room) => {
                        const roomRacks = racks.filter((r) => r.roomId === room.id)
                        const isActiveRoom = selection?.type === 'room' && selection.id === room.id
                        return (
                          <TreeNode
                            key={room.id}
                            label={room.name}
                            icon={<IconDoor size={12} />}
                            depth={1}
                            active={isActiveRoom}
                            onClick={() => setSelection({ type: 'room', id: room.id })}
                            badge={roomRacks.length > 0 ? <Badge size="xs" variant="outline" color="teal">{roomRacks.length}</Badge> : undefined}
                            defaultOpen={false}
                          >
                            {roomRacks.length > 0 && roomRacks.map((rack) => (
                              <TreeNode
                                key={rack.id}
                                label={rack.name}
                                icon={<IconLayersIntersect size={12} />}
                                depth={2}
                                active={selection?.type === 'rack' && selection.id === rack.id}
                                onClick={() => setSelection({ type: 'rack', id: rack.id })}
                              />
                            ))}
                          </TreeNode>
                        )
                      })}

                      {noRoomRacks.length > 0 && (
                        <TreeNode
                          key={`${dc.id}-noroomracks`}
                          label="Racks (no room)"
                          icon={<IconLayersIntersect size={12} />}
                          depth={1}
                          onClick={() => {}}
                          badge={<Badge size="xs" variant="outline" color="gray">{noRoomRacks.length}</Badge>}
                          defaultOpen={false}
                        >
                          {noRoomRacks.map((rack) => (
                            <TreeNode
                              key={rack.id}
                              label={rack.name}
                              icon={<IconLayersIntersect size={12} />}
                              depth={2}
                              active={selection?.type === 'rack' && selection.id === rack.id}
                              onClick={() => setSelection({ type: 'rack', id: rack.id })}
                            />
                          ))}
                        </TreeNode>
                      )}
                    </TreeNode>
                  )
                })}
              </Stack>
            )}
          </ScrollArea>
        </div>

        {/* Right detail panel */}
        <div style={{ flex: 1, height: '100%', overflow: 'auto', padding: '16px 20px' }}>
          <DetailPanel
            selection={selection}
            dcs={dcs}
            rooms={rooms}
            racks={racks}
            onEdit={openEditModal}
            onDelete={openDelete}
            onCreateRoom={openCreateRoom}
            onCreateRack={openCreateRack}
          />
        </div>
      </Group>

      {/* Delete confirmation */}
      <Modal
        opened={deleteOpened}
        onClose={closeDelete}
        title="Confirm Deletion"
        centered
        withinPortal
        zIndex={2000}
        size="sm"
      >
        <Stack gap="md">
          <Text size="sm">
            Are you sure you want to delete <Text component="span" fw={700}>{getDeleteLabel()}</Text>?
          </Text>
          {selection?.type !== 'rack' && (
            <Text size="sm" c="dimmed">
              Deletion will be blocked if this item still has children. Remove them first.
            </Text>
          )}
          <Divider />
          <Group justify="flex-end">
            <Button variant="default" onClick={closeDelete}>Cancel</Button>
            <Button color="red" onClick={() => void handleDelete()} loading={deleting}>Delete</Button>
          </Group>
        </Stack>
      </Modal>

      {/* Create/Edit Datacenter */}
      {(activeModal?.type === 'create-dc' || activeModal?.type === 'edit-dc') && (
        <DatacenterFormModal
          opened
          onClose={() => setActiveModal(null)}
          initial={activeModal.type === 'edit-dc' ? activeModal.dc : undefined}
          onSaved={(saved) => {
            if (activeModal.type === 'edit-dc') {
              setDcs((prev) => prev.map((d) => d.id === saved.id ? saved : d))
            } else {
              setDcs((prev) => [...prev, saved].sort((a, b) => a.name.localeCompare(b.name)))
              setSelection({ type: 'dc', id: saved.id })
            }
          }}
        />
      )}

      {/* Create/Edit Room */}
      {(activeModal?.type === 'create-room' || activeModal?.type === 'edit-room') && (
        <RoomFormModal
          opened
          onClose={() => setActiveModal(null)}
          initial={activeModal.type === 'edit-room' ? activeModal.room : undefined}
          datacenterId={activeModal.type === 'create-room' ? activeModal.datacenterId : activeModal.room.datacenterId}
          onSaved={(saved) => {
            if (activeModal.type === 'edit-room') {
              setRooms((prev) => prev.map((r) => r.id === saved.id ? saved : r))
            } else {
              setRooms((prev) => [...prev, saved].sort((a, b) => a.name.localeCompare(b.name)))
              setSelection({ type: 'room', id: saved.id })
            }
          }}
        />
      )}

      {/* Create/Edit Rack */}
      {(activeModal?.type === 'create-rack' || activeModal?.type === 'edit-rack') && (
        <RackFormModal
          opened
          onClose={() => setActiveModal(null)}
          initial={activeModal.type === 'edit-rack' ? activeModal.rack : undefined}
          datacenterId={activeModal.type === 'create-rack' ? activeModal.datacenterId : activeModal.rack.datacenterId}
          roomId={activeModal.type === 'create-rack' ? activeModal.roomId : activeModal.rack.roomId}
          onSaved={(saved) => {
            if (activeModal.type === 'edit-rack') {
              setRacks((prev) => prev.map((r) => r.id === saved.id ? saved : r))
            } else {
              setRacks((prev) => [...prev, saved].sort((a, b) => a.name.localeCompare(b.name)))
              setSelection({ type: 'rack', id: saved.id })
            }
          }}
        />
      )}
    </>
  )
}
