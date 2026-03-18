import { useCallback, useEffect, useState } from 'react'
import {
  ActionIcon, Badge, Button, Card, Group, Modal, Stack,
  Table, Text, TextInput, Textarea, Tooltip,
} from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { notifications } from '@mantine/notifications'
import { useNavigate } from 'react-router-dom'
import {
  IconBuildingCommunity, IconPlus, IconRefresh, IconTrash,
} from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import { useAuth } from '@/presentation/contexts/AuthContext'
import type { Team } from '@/domain/team/types'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'

function CreateTeamModal({
  opened, onClose, onCreated,
}: { opened: boolean; onClose: () => void; onCreated: (team: Team) => void }) {
  const { teams } = useApp()
  const { currentUser } = useAuth()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const handleCreate = async () => {
    if (!name.trim() || !currentUser) return
    try {
      setSubmitting(true)
      const team = await teams.create.execute({
        name: name.trim(),
        description: description.trim(),
        ownerIds: [currentUser.id],
        memberIds: [currentUser.id],
      })
      notifications.show({ title: 'Team created', message: team.name, color: 'green' })
      onCreated(team)
      setName('')
      setDescription('')
      onClose()
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal opened={opened} onClose={onClose} title="Create Team" centered withinPortal zIndex={2000}>
      <Stack gap="md">
        <TextInput
          label="Team Name"
          placeholder="e.g. AI Team"
          value={name}
          onChange={(e) => setName(e.target.value)}
          required
        />
        <Textarea
          label="Description"
          placeholder="What does this team work on?"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          minRows={3}
        />
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>Cancel</Button>
          <Button onClick={() => void handleCreate()} loading={submitting} disabled={!name.trim()}>
            Create
          </Button>
        </Group>
      </Stack>
    </Modal>
  )
}

export function TeamsPage() {
  const { teams, servers } = useApp()
  const { currentUser } = useAuth()
  const navigate = useNavigate()
  const [teamList, setTeamList] = useState<Team[]>([])
  const [serverCountByTeam, setServerCountByTeam] = useState<Record<string, number>>({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [createOpened, { open: openCreate, close: closeCreate }] = useDisclosure(false)
  const isAdmin = currentUser?.role === 'admin'

  const load = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const [teamResult, allServers] = await Promise.all([
        teams.list.execute(),
        servers.list.execute(),
      ])
      setTeamList(teamResult)
      const counts: Record<string, number> = {}
      for (const s of allServers) {
        if (s.ownerTeamId) {
          counts[s.ownerTeamId] = (counts[s.ownerTeamId] ?? 0) + 1
        }
      }
      setServerCountByTeam(counts)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }, [teams.list, servers.list])

  useEffect(() => { void load() }, [load])

  const handleDelete = async (team: Team) => {
    const serverCount = serverCountByTeam[team.id] ?? 0
    if (serverCount > 0) {
      notifications.show({
        title: 'Cannot delete',
        message: `${team.name} has ${serverCount} server(s) assigned. Unassign them first.`,
        color: 'red',
      })
      return
    }
    if (!confirm(`Delete team "${team.name}"?`)) return
    try {
      await teams.deleteTeam.execute(team.id)
      setTeamList((prev) => prev.filter((t) => t.id !== team.id))
      notifications.show({ title: 'Team deleted', message: team.name, color: 'gray' })
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    }
  }

  if (loading) return <LoadingState />
  if (error) return <ErrorState message={error} onRetry={() => void load()} />

  return (
    <>
      <PageHeader
        title="Teams"
        subtitle={`${teamList.length} teams`}
        actions={
          <Group gap="sm">
            <ActionIcon variant="default" onClick={() => void load()}><IconRefresh size={16} /></ActionIcon>
            {isAdmin && (
              <Button leftSection={<IconPlus size={14} />} size="sm" onClick={openCreate}>
                New Team
              </Button>
            )}
          </Group>
        }
      />

      {teamList.length === 0 ? (
        <EmptyState message="No teams found." />
      ) : (
        <Card withBorder radius="md" p={0}>
          <Table highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Team</Table.Th>
                <Table.Th>Owners</Table.Th>
                <Table.Th>Members</Table.Th>
                <Table.Th>Servers</Table.Th>
                {isAdmin && <Table.Th></Table.Th>}
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {teamList.map((team) => {
                const srvCount = serverCountByTeam[team.id] ?? 0
                return (
                  <Table.Tr
                    key={team.id}
                    style={{ cursor: 'pointer' }}
                    onClick={() => navigate(`/teams/${team.id}`)}
                  >
                    <Table.Td>
                      <Group gap="xs">
                        <IconBuildingCommunity size={16} color="var(--mantine-color-blue-5)" />
                        <Stack gap={0}>
                          <Text size="sm" fw={500}>{team.name}</Text>
                          <Text size="xs" c="dimmed" lineClamp={1}>{team.description}</Text>
                        </Stack>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Badge variant="outline" size="sm">{team.ownerIds.length}</Badge>
                    </Table.Td>
                    <Table.Td>
                      <Badge variant="outline" size="sm">{team.memberIds.length}</Badge>
                    </Table.Td>
                    <Table.Td>
                      <Badge color={srvCount > 0 ? 'blue' : 'gray'} variant="light" size="sm">{srvCount}</Badge>
                    </Table.Td>
                    {isAdmin && (
                      <Table.Td onClick={(e) => e.stopPropagation()}>
                        <Tooltip label={srvCount > 0 ? 'Unassign servers first' : 'Delete team'}>
                          <ActionIcon
                            variant="subtle"
                            color="red"
                            size="sm"
                            disabled={srvCount > 0}
                            onClick={() => void handleDelete(team)}
                          >
                            <IconTrash size={14} />
                          </ActionIcon>
                        </Tooltip>
                      </Table.Td>
                    )}
                  </Table.Tr>
                )
              })}
            </Table.Tbody>
          </Table>
        </Card>
      )}

      <CreateTeamModal
        opened={createOpened}
        onClose={closeCreate}
        onCreated={(team) => setTeamList((prev) => [...prev, team])}
      />
    </>
  )
}
