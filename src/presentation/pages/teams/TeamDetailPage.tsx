import { useCallback, useEffect, useState } from 'react'
import {
  ActionIcon, Badge, Button, Card, Group, Modal, Select,
  SimpleGrid, Stack, Table, Text, ThemeIcon, Tooltip,
} from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { notifications } from '@mantine/notifications'
import { useNavigate, useParams } from 'react-router-dom'
import {
  IconArrowLeft, IconBuildingCommunity, IconServer, IconTrash,
  IconUserCheck, IconUserPlus, IconCrown,
} from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import { useAuth } from '@/presentation/contexts/AuthContext'
import type { Team } from '@/domain/team/types'
import type { Server } from '@/domain/server/types'
import { PageHeader } from '@/presentation/components/PageHeader'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { formatRelative } from '@/shared/utils/time'

function AddMemberModal({
  team, opened, onClose, onAdded,
}: { team: Team; opened: boolean; onClose: () => void; onAdded: (team: Team) => void }) {
  const { teams } = useApp()
  const { users } = useAuth()
  const [userId, setUserId] = useState<string | null>(null)
  const [role, setRole] = useState<'member' | 'owner'>('member')
  const [submitting, setSubmitting] = useState(false)

  const existingIds = new Set([...team.memberIds, ...team.ownerIds])
  const available = users
    .filter((u) => u.status === 'active' && !existingIds.has(u.id))
    .map((u) => ({ value: u.id, label: `${u.displayName} (${u.username})` }))

  const handleAdd = async () => {
    if (!userId) return
    try {
      setSubmitting(true)
      let updated: Team
      if (role === 'owner') {
        updated = await teams.addOwner.execute(team.id, userId)
      } else {
        updated = await teams.addMember.execute(team.id, userId)
      }
      const userName = users.find((u) => u.id === userId)?.displayName ?? userId
      notifications.show({ title: 'Member added', message: `${userName} joined ${team.name}`, color: 'green' })
      onAdded(updated)
      setUserId(null)
      onClose()
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal opened={opened} onClose={onClose} title="Add Member" centered withinPortal zIndex={2000}>
      <Stack gap="md">
        <Select
          label="User"
          data={available}
          value={userId}
          onChange={setUserId}
          placeholder="Select user..."
          searchable
          allowDeselect={false}
          comboboxProps={{ withinPortal: false }}
        />
        <Select
          label="Role in Team"
          data={[
            { value: 'member', label: 'Member' },
            { value: 'owner', label: 'Owner' },
          ]}
          value={role}
          onChange={(v) => setRole((v as 'member' | 'owner') ?? 'member')}
          allowDeselect={false}
          comboboxProps={{ withinPortal: false }}
        />
        {available.length === 0 && (
          <Text size="sm" c="dimmed">All available users are already in this team.</Text>
        )}
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>Cancel</Button>
          <Button onClick={() => void handleAdd()} loading={submitting} disabled={!userId}>
            Add
          </Button>
        </Group>
      </Stack>
    </Modal>
  )
}

export function TeamDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { teams, servers } = useApp()
  const { users, currentUser } = useAuth()
  const [team, setTeam] = useState<Team | null>(null)
  const [teamServers, setTeamServers] = useState<Server[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [addMemberOpened, { open: openAddMember, close: closeAddMember }] = useDisclosure(false)

  const isAdmin = currentUser?.role === 'admin'
  const isOwner = team ? team.ownerIds.includes(currentUser?.id ?? '') : false
  const canManage = isAdmin || isOwner

  const load = useCallback(async () => {
    if (!id) return
    try {
      setLoading(true)
      setError(null)
      const [teamResult, srvResult] = await Promise.all([
        teams.get.execute(id),
        servers.list.execute({ teamId: id }),
      ])
      if (!teamResult) {
        setError('Team not found')
        return
      }
      setTeam(teamResult)
      setTeamServers(srvResult)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [id, teams.get, servers.list])

  useEffect(() => { void load() }, [load])

  const handleRemoveMember = async (userId: string) => {
    if (!team) return
    const user = users.find((u) => u.id === userId)
    if (!confirm(`Remove ${user?.displayName ?? userId} from ${team.name}?`)) return
    try {
      const updated = await teams.removeMember.execute(team.id, userId)
      setTeam(updated)
      notifications.show({ title: 'Member removed', message: `${user?.displayName ?? userId} removed`, color: 'gray' })
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    }
  }

  const handleToggleOwner = async (userId: string) => {
    if (!team) return
    try {
      const isCurrentlyOwner = team.ownerIds.includes(userId)
      const updated = isCurrentlyOwner
        ? await teams.removeOwner.execute(team.id, userId)
        : await teams.addOwner.execute(team.id, userId)
      setTeam(updated)
      notifications.show({ title: 'Role updated', message: `Owner status toggled`, color: 'blue' })
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    }
  }

  if (loading) return <LoadingState />
  if (error || !team) return <ErrorState message={error ?? 'Team not found'} onRetry={() => void load()} />

  const allMembers = Array.from(new Set([...team.memberIds, ...team.ownerIds]))

  return (
    <>
      <PageHeader
        title={team.name}
        subtitle={team.description || 'No description'}
        actions={
          <Button variant="subtle" leftSection={<IconArrowLeft size={14} />} onClick={() => navigate('/teams')}>
            Back to Teams
          </Button>
        }
      />

      <SimpleGrid cols={{ base: 1, sm: 3 }} mb="lg">
        <Card withBorder radius="md">
          <Group gap="sm">
            <ThemeIcon color="blue" variant="light"><IconBuildingCommunity size={16} /></ThemeIcon>
            <Stack gap={0}>
              <Text size="xs" c="dimmed">Owners</Text>
              <Text fw={600}>{team.ownerIds.length}</Text>
            </Stack>
          </Group>
        </Card>
        <Card withBorder radius="md">
          <Group gap="sm">
            <ThemeIcon color="teal" variant="light"><IconUserCheck size={16} /></ThemeIcon>
            <Stack gap={0}>
              <Text size="xs" c="dimmed">Members</Text>
              <Text fw={600}>{team.memberIds.length}</Text>
            </Stack>
          </Group>
        </Card>
        <Card withBorder radius="md">
          <Group gap="sm">
            <ThemeIcon color="violet" variant="light"><IconServer size={16} /></ThemeIcon>
            <Stack gap={0}>
              <Text size="xs" c="dimmed">Servers</Text>
              <Text fw={600}>{teamServers.length}</Text>
            </Stack>
          </Group>
        </Card>
      </SimpleGrid>

      <Stack gap="lg">
        <Card withBorder radius="md">
          <Group justify="space-between" mb="md">
            <Text fw={600}>Members</Text>
            {canManage && (
              <Button size="xs" leftSection={<IconUserPlus size={12} />} onClick={openAddMember}>
                Add Member
              </Button>
            )}
          </Group>
          {allMembers.length === 0 ? (
            <Text size="sm" c="dimmed">No members yet.</Text>
          ) : (
            <Table>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>User</Table.Th>
                  <Table.Th>Role</Table.Th>
                  {canManage && <Table.Th>Actions</Table.Th>}
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {allMembers.map((userId) => {
                  const user = users.find((u) => u.id === userId)
                  const isOwnerOfTeam = team.ownerIds.includes(userId)
                  return (
                    <Table.Tr key={userId}>
                      <Table.Td>
                        <Stack gap={0}>
                          <Text size="sm" fw={500}>{user?.displayName ?? userId}</Text>
                          <Text size="xs" c="dimmed">{user?.username}</Text>
                        </Stack>
                      </Table.Td>
                      <Table.Td>
                        {isOwnerOfTeam ? (
                          <Badge color="orange" leftSection={<IconCrown size={10} />} size="sm">Owner</Badge>
                        ) : (
                          <Badge color="gray" variant="outline" size="sm">Member</Badge>
                        )}
                      </Table.Td>
                      {canManage && (
                        <Table.Td>
                          <Group gap={4}>
                            <Tooltip label={isOwnerOfTeam ? 'Remove owner role' : 'Promote to owner'}>
                              <ActionIcon
                                variant="subtle"
                                size="sm"
                                color={isOwnerOfTeam ? 'orange' : 'gray'}
                                onClick={() => void handleToggleOwner(userId)}
                              >
                                <IconCrown size={12} />
                              </ActionIcon>
                            </Tooltip>
                            <Tooltip label="Remove from team">
                              <ActionIcon
                                variant="subtle"
                                size="sm"
                                color="red"
                                onClick={() => void handleRemoveMember(userId)}
                              >
                                <IconTrash size={12} />
                              </ActionIcon>
                            </Tooltip>
                          </Group>
                        </Table.Td>
                      )}
                    </Table.Tr>
                  )
                })}
              </Table.Tbody>
            </Table>
          )}
        </Card>

        <Card withBorder radius="md">
          <Group justify="space-between" mb="md">
            <Text fw={600}>Assigned Servers</Text>
            <Button size="xs" variant="subtle" onClick={() => navigate('/servers', { state: { allocationSearch: team.name } })}>
              View all servers
            </Button>
          </Group>
          {teamServers.length === 0 ? (
            <Text size="sm" c="dimmed">No servers assigned to this team.</Text>
          ) : (
            <Table>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Hostname</Table.Th>
                  <Table.Th>Status</Table.Th>
                  <Table.Th>IP</Table.Th>
                  <Table.Th>GPU</Table.Th>
                  <Table.Th>Last Seen</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {teamServers.map((s) => (
                  <Table.Tr key={s.id}>
                    <Table.Td><Text size="sm" fw={500}>{s.hostname}</Text></Table.Td>
                    <Table.Td>
                      <Badge
                        color={s.status === 'live' ? 'green' : s.status === 'error' ? 'red' : s.status === 'offline' ? 'gray' : 'yellow'}
                        variant="light" size="sm"
                      >
                        {s.status}
                      </Badge>
                    </Table.Td>
                    <Table.Td><Text size="sm" ff="mono">{s.ip}</Text></Table.Td>
                    <Table.Td>
                      {s.gpuCount > 0
                        ? <Text size="sm">{s.gpuType} ×{s.gpuCount}</Text>
                        : <Text size="sm" c="dimmed">—</Text>
                      }
                    </Table.Td>
                    <Table.Td><Text size="xs" c="dimmed">{formatRelative(s.lastSeenAt)}</Text></Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Card>
      </Stack>

      <AddMemberModal
        team={team}
        opened={addMemberOpened}
        onClose={closeAddMember}
        onAdded={(updated) => setTeam(updated)}
      />
    </>
  )
}
