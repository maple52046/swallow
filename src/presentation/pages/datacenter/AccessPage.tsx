import { useEffect, useState } from 'react'
import {
  Table, Badge, Group, Text, Button, Stack, Modal, TextInput, Select, NumberInput,
} from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { notifications } from '@mantine/notifications'
import { IconLink, IconPlus } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { Connection } from '@/domain/asset/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { formatRelative } from '@/shared/utils/time'

export function AccessPage() {
  const { datacenter } = useApp()

  const [connections, setConnections] = useState<Connection[]>([])
  const [loading, setLoading] = useState(true)
  const [addOpened, { open: openAdd, close: closeAdd }] = useDisclosure(false)

  const [formName, setFormName] = useState('')
  const [formHost, setFormHost] = useState('')
  const [formPort, setFormPort] = useState<number>(22)
  const [formUsername, setFormUsername] = useState('root')
  const [formType, setFormType] = useState<'ssh' | 'bastion' | 'vpn'>('ssh')
  const [saving, setSaving] = useState(false)

  const load = () => {
    datacenter.listConnections.execute()
      .then((conns) => { setConnections(conns) })
      .catch(() => null)
      .finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const handleSave = async () => {
    setSaving(true)
    try {
      await datacenter.upsertConnection.execute({
        name: formName,
        host: formHost,
        port: formPort,
        type: formType,
        username: formUsername,
        labels: [],
      })
      notifications.show({ title: 'Connection saved', message: formName, color: 'green' })
      closeAdd()
      load()
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    } finally {
      setSaving(false)
    }
  }

  if (loading) return <LoadingState />

  return (
    <>
      <PageHeader
        title={t('access.title')}
        subtitle="Manage SSH connections and access policies"
        actions={
          <Button size="sm" leftSection={<IconPlus size={14} />} onClick={openAdd}>
            {t('access.addConnection')}
          </Button>
        }
      />

      <Group gap="sm" mb="md" align="center">
        <IconLink size={16} />
        <Text fw={500}>{t('access.connections')} ({connections.length})</Text>
      </Group>

      {connections.length === 0 ? (
        <EmptyState message={t('access.empty.connections')} />
      ) : (
        <Table highlightOnHover>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>{t('common.name')}</Table.Th>
              <Table.Th>Host</Table.Th>
              <Table.Th>Port</Table.Th>
              <Table.Th>User</Table.Th>
              <Table.Th>{t('common.type')}</Table.Th>
              <Table.Th>Labels</Table.Th>
              <Table.Th>Created</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {connections.map((conn) => (
              <Table.Tr key={conn.id}>
                <Table.Td><Text size="sm" fw={500}>{conn.name}</Text></Table.Td>
                <Table.Td><Text size="sm" ff="mono">{conn.host}</Text></Table.Td>
                <Table.Td><Text size="sm">{conn.port}</Text></Table.Td>
                <Table.Td><Text size="sm">{conn.username}</Text></Table.Td>
                <Table.Td><Badge size="sm" variant="outline">{conn.type}</Badge></Table.Td>
                <Table.Td>
                  <Group gap={4}>
                    {conn.labels.slice(0, 3).map((l) => <Badge key={l} size="xs" variant="dot">{l}</Badge>)}
                  </Group>
                </Table.Td>
                <Table.Td><Text size="sm" c="dimmed">{formatRelative(conn.createdAt)}</Text></Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      )}

      <Modal opened={addOpened} onClose={closeAdd} title={t('access.addConnection')}>
        <Stack gap="md">
          <TextInput label={t('common.name')} value={formName} onChange={(e) => setFormName(e.target.value)} required />
          <TextInput label="Host" placeholder="192.168.1.100" value={formHost} onChange={(e) => setFormHost(e.target.value)} required />
          <NumberInput label="Port" value={formPort} onChange={(v) => setFormPort(Number(v))} min={1} max={65535} />
          <TextInput label="Username" value={formUsername} onChange={(e) => setFormUsername(e.target.value)} />
          <Select
            label={t('common.type')}
            data={[
              { value: 'ssh', label: t('access.connectionType.ssh') },
              { value: 'bastion', label: t('access.connectionType.bastion') },
              { value: 'vpn', label: t('access.connectionType.vpn') },
            ]}
            value={formType}
            onChange={(v) => v && setFormType(v as 'ssh' | 'bastion' | 'vpn')}
          />
          <Group justify="flex-end">
            <Button variant="default" onClick={closeAdd}>{t('common.cancel')}</Button>
            <Button onClick={() => void handleSave()} loading={saving}>{t('common.save')}</Button>
          </Group>
        </Stack>
      </Modal>
    </>
  )
}
