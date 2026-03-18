import { useState } from 'react'
import { Badge, Button, Group, Modal, Select, Stack, Text } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconUserCheck, IconBuildingCommunity, IconX } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import { useAuth } from '@/presentation/contexts/AuthContext'
import type { Server } from '@/domain/server/types'
import type { Team } from '@/domain/team/types'
import { getAllocationState } from '@/domain/server/types'

interface AllocationModalProps {
  server: Server | null
  teams: Team[]
  opened: boolean
  onClose: () => void
  onUpdated: (server: Server) => void
}

export function AllocationModal({ server, teams, opened, onClose, onUpdated }: AllocationModalProps) {
  const { servers } = useApp()
  const { users, currentUser } = useAuth()
  const [mode, setMode] = useState<'team' | 'user' | null>(null)
  const [selectedTeamId, setSelectedTeamId] = useState<string | null>(null)
  const [selectedUserId, setSelectedUserId] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  const isAdmin = currentUser?.role === 'admin'
  const allocationState = server ? getAllocationState(server) : 'free'

  const teamOptions = teams.map((t) => ({ value: t.id, label: t.name }))
  const userOptions = users
    .filter((u) => u.status === 'active')
    .map((u) => ({ value: u.id, label: `${u.displayName} (${u.username})` }))

  const handleClose = () => {
    setMode(null)
    setSelectedTeamId(null)
    setSelectedUserId(null)
    onClose()
  }

  const handleAssign = async () => {
    if (!server) return
    try {
      setSubmitting(true)
      let updated: Server
      if (mode === 'team' && selectedTeamId) {
        updated = await servers.assignToTeam.execute(server.id, selectedTeamId)
        const teamName = teams.find((t) => t.id === selectedTeamId)?.name ?? selectedTeamId
        notifications.show({ title: 'Assigned', message: `${server.hostname} → ${teamName}`, color: 'blue' })
      } else if (mode === 'user' && selectedUserId) {
        updated = await servers.assignToUser.execute(server.id, selectedUserId)
        const userName = users.find((u) => u.id === selectedUserId)?.displayName ?? selectedUserId
        notifications.show({ title: 'Assigned', message: `${server.hostname} → ${userName}`, color: 'orange' })
      } else {
        return
      }
      onUpdated(updated)
      handleClose()
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    } finally {
      setSubmitting(false)
    }
  }

  const handleUnassign = async () => {
    if (!server) return
    try {
      setSubmitting(true)
      const updated = await servers.unassign.execute(server.id)
      notifications.show({ title: 'Unassigned', message: `${server.hostname} is now free`, color: 'gray' })
      onUpdated(updated)
      handleClose()
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    } finally {
      setSubmitting(false)
    }
  }

  if (!server) return null

  const canSubmit = (mode === 'team' && !!selectedTeamId) || (mode === 'user' && !!selectedUserId)

  return (
    <Modal
      opened={opened}
      onClose={handleClose}
      title={<Text fw={600}>Allocation — {server.hostname}</Text>}
      centered
      withinPortal
      zIndex={2000}
    >
      <Stack gap="md">
        <Group gap="sm">
          <Text size="sm" c="dimmed">Current:</Text>
          {allocationState === 'free' && <Badge color="gray" variant="outline">Free</Badge>}
          {allocationState === 'team' && (
            <Badge color="blue" variant="light">
              {teams.find((t) => t.id === server.ownerTeamId)?.name ?? server.ownerTeamId}
            </Badge>
          )}
          {allocationState === 'user' && (
            <Badge color="orange" variant="light">
              {users.find((u) => u.id === server.ownerUserId)?.displayName ?? server.ownerUserId}
            </Badge>
          )}
        </Group>

        {!isAdmin && (
          <Text size="sm" c="dimmed">Only admins can change allocation.</Text>
        )}

        {isAdmin && (
          <>
            <Group gap="sm">
              <Button
                variant={mode === 'team' ? 'filled' : 'default'}
                leftSection={<IconBuildingCommunity size={14} />}
                onClick={() => setMode('team')}
                size="sm"
                color="blue"
              >
                Assign to Team
              </Button>
              <Button
                variant={mode === 'user' ? 'filled' : 'default'}
                leftSection={<IconUserCheck size={14} />}
                onClick={() => setMode('user')}
                size="sm"
                color="orange"
              >
                Assign to User
              </Button>
            </Group>

            {mode === 'team' && (
              <Select
                label="Select Team"
                data={teamOptions}
                value={selectedTeamId}
                onChange={setSelectedTeamId}
                placeholder="Choose a team..."
                allowDeselect={false}
                searchable
                comboboxProps={{ withinPortal: false }}
              />
            )}

            {mode === 'user' && (
              <Select
                label="Select User"
                data={userOptions}
                value={selectedUserId}
                onChange={setSelectedUserId}
                placeholder="Choose a user..."
                allowDeselect={false}
                searchable
                comboboxProps={{ withinPortal: false }}
              />
            )}

            <Group justify="space-between" mt="sm">
              {allocationState !== 'free' && (
                <Button
                  variant="subtle"
                  color="red"
                  leftSection={<IconX size={14} />}
                  onClick={() => void handleUnassign()}
                  loading={submitting && !mode}
                >
                  Unassign
                </Button>
              )}
              {allocationState === 'free' && <div />}
              <Group gap="sm">
                <Button variant="default" onClick={handleClose}>Cancel</Button>
                <Button
                  onClick={() => void handleAssign()}
                  loading={submitting && !!mode}
                  disabled={!canSubmit}
                >
                  Assign
                </Button>
              </Group>
            </Group>
          </>
        )}

        {!isAdmin && (
          <Group justify="flex-end">
            <Button variant="default" onClick={handleClose}>Close</Button>
          </Group>
        )}
      </Stack>
    </Modal>
  )
}
