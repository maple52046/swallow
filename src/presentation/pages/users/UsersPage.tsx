import { Badge, Card, Group, Select, Stack, Table, Text } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { PageHeader } from '@/presentation/components/PageHeader'
import { useAuth, type UserRole } from '@/presentation/contexts/AuthContext'

const roleOptions = [
  { value: 'admin', label: 'admin' },
  { value: 'owner', label: 'owner' },
  { value: 'member', label: 'member' },
]

function roleColor(role: UserRole) {
  if (role === 'admin') return 'red'
  if (role === 'owner') return 'blue'
  return 'gray'
}

export function UsersPage() {
  const { users, updateUserRole, currentUser } = useAuth()

  const handleRoleChange = (username: string, role: string | null) => {
    if (!role) return
    updateUserRole(username, role as UserRole)
    notifications.show({
      title: 'Role updated',
      message: `${username} is now ${role}`,
      color: 'green',
    })
  }

  return (
    <>
      <PageHeader title="Users" subtitle="Admin-only mock user management" />

      <Card withBorder radius="md">
        <Stack gap="sm">
          <Group justify="space-between">
            <Text size="sm" c="dimmed">
              Signed in as {currentUser?.displayName} ({currentUser?.role})
            </Text>
            <Badge color="red" variant="light">admin only</Badge>
          </Group>

          <Table withTableBorder withColumnBorders highlightOnHover>
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Username</Table.Th>
                <Table.Th>Display Name</Table.Th>
                <Table.Th>Role</Table.Th>
                <Table.Th>Status</Table.Th>
                <Table.Th>Change Role</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {users.map((u) => (
                <Table.Tr key={u.id}>
                  <Table.Td>{u.username}</Table.Td>
                  <Table.Td>{u.displayName}</Table.Td>
                  <Table.Td>
                    <Badge color={roleColor(u.role)} variant="light">{u.role}</Badge>
                  </Table.Td>
                  <Table.Td>
                    <Badge color={u.status === 'active' ? 'green' : 'gray'} variant="outline">{u.status}</Badge>
                  </Table.Td>
                  <Table.Td>
                    <Select
                      value={u.role}
                      data={roleOptions}
                      onChange={(value) => handleRoleChange(u.username, value)}
                      allowDeselect={false}
                      w={140}
                      size="xs"
                    />
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Stack>
      </Card>
    </>
  )
}
