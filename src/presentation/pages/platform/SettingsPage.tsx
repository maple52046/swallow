import { useEffect, useState } from 'react'
import {
  ActionIcon, Badge, Button, Card, Divider, Group, Modal, Radio,
  SegmentedControl, Stack, Table, Text, Textarea, TextInput, ThemeIcon,
} from '@mantine/core'
import { useMantineColorScheme } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconKey, IconMoon, IconPlus, IconSun, IconTrash } from '@tabler/icons-react'
import { saveColorScheme } from '@/presentation/app/theme'
import { t } from '@/presentation/app/i18n'
import { useApp } from '@/di/AppProvider'
import type { SSHKey } from '@/domain/asset/types'
import { PageHeader } from '@/presentation/components/PageHeader'
import { formatRelative } from '@/shared/utils/time'

const SSH_KEY_TYPES = ['ssh-ed25519', 'ssh-rsa', 'ecdsa-sha2-nistp256', 'ecdsa-sha2-nistp384', 'ecdsa-sha2-nistp521', 'sk-ssh-ed25519', 'sk-ecdsa-sha2-nistp256']

function validatePublicKey(value: string): string | null {
  const trimmed = value.trim()
  if (!trimmed) return 'Public key is required'
  const valid = SSH_KEY_TYPES.some((t) => trimmed.startsWith(t + ' '))
  if (!valid) return 'Invalid SSH public key format. Must start with a valid key type (e.g. ssh-ed25519, ssh-rsa)'
  return null
}

export function SettingsPage() {
  const { colorScheme, setColorScheme } = useMantineColorScheme()
  const { datacenter } = useApp()
  const [sshKeys, setSshKeys] = useState<SSHKey[]>([])

  // modal states
  const [methodOpen, setMethodOpen] = useState(false)
  const [selectedMode, setSelectedMode] = useState<'create' | 'import'>('create')
  const [createOpen, setCreateOpen] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<SSHKey | null>(null)

  // create form
  const [createName, setCreateName] = useState('')

  // import form
  const [importName, setImportName] = useState('')
  const [importKey, setImportKey] = useState('')
  const [importNameError, setImportNameError] = useState('')
  const [importKeyError, setImportKeyError] = useState('')
  const [importLoading, setImportLoading] = useState(false)

  // delete state
  const [deleteLoading, setDeleteLoading] = useState(false)

  useEffect(() => {
    datacenter.listSSHKeys.execute()
      .then(setSshKeys)
      .catch(() => null)
  }, [datacenter.listSSHKeys])

  const handleSchemeChange = (value: string) => {
    const cs = value as 'light' | 'dark'
    setColorScheme(cs)
    saveColorScheme(cs)
  }

  // ── Method modal ──────────────────────────────────────────────────────────

  const handleMethodContinue = () => {
    setMethodOpen(false)
    if (selectedMode === 'create') {
      setCreateName('')
      setCreateOpen(true)
    } else {
      setImportName('')
      setImportKey('')
      setImportNameError('')
      setImportKeyError('')
      setImportOpen(true)
    }
  }

  // ── Create modal ──────────────────────────────────────────────────────────

  const handleCreate = () => {
    notifications.show({
      title: 'Not implemented',
      message: 'SSH key generation is not implemented yet.',
      color: 'orange',
    })
    setCreateOpen(false)
  }

  // ── Import modal ──────────────────────────────────────────────────────────

  const handleImport = async () => {
    let hasError = false
    if (!importName.trim()) {
      setImportNameError('Name is required')
      hasError = true
    } else {
      setImportNameError('')
    }
    const keyErr = validatePublicKey(importKey)
    if (keyErr) {
      setImportKeyError(keyErr)
      hasError = true
    } else {
      setImportKeyError('')
    }
    if (hasError) return

    setImportLoading(true)
    try {
      const newKey = await datacenter.importSSHKey.execute({ name: importName.trim(), publicKey: importKey.trim() })
      setSshKeys((prev) => [...prev, newKey])
      setImportOpen(false)
      notifications.show({ title: 'SSH key imported', message: 'SSH key imported successfully.', color: 'green' })
    } catch {
      notifications.show({ title: 'Error', message: 'Failed to import SSH key.', color: 'red' })
    } finally {
      setImportLoading(false)
    }
  }

  // ── Delete modal ──────────────────────────────────────────────────────────

  const handleDelete = async () => {
    if (!deleteTarget) return
    setDeleteLoading(true)
    try {
      await datacenter.deleteSSHKey.execute(deleteTarget.id)
      setSshKeys((prev) => prev.filter((k) => k.id !== deleteTarget.id))
      setDeleteTarget(null)
      notifications.show({ title: 'SSH key deleted', message: 'SSH key deleted successfully.', color: 'green' })
    } catch {
      notifications.show({ title: 'Error', message: 'Failed to delete SSH key.', color: 'red' })
    } finally {
      setDeleteLoading(false)
    }
  }

  return (
    <>
      <PageHeader
        title={t('settings.title')}
        subtitle="Configure dashboard preferences and manage your credentials"
      />

      <Stack gap="xl" maw={600}>
        <Card withBorder>
          <Text fw={500} mb="md">{t('settings.appearance')}</Text>
          <Stack gap="md">
            <Group justify="space-between">
              <Stack gap={0}>
                <Text size="sm">{t('settings.theme')}</Text>
                <Text size="xs" c="dimmed">Choose between light and dark mode</Text>
              </Stack>
              <SegmentedControl
                value={colorScheme}
                onChange={handleSchemeChange}
                data={[
                  { label: <Group gap={4}><IconSun size={14} />{t('settings.lightMode')}</Group>, value: 'light' },
                  { label: <Group gap={4}><IconMoon size={14} />{t('settings.darkMode')}</Group>, value: 'dark' },
                ]}
              />
            </Group>
          </Stack>
        </Card>

        <Card withBorder>
          <Text fw={500} mb="md">{t('settings.language')}</Text>
          <Group justify="space-between">
            <Stack gap={0}>
              <Text size="sm">English</Text>
              <Text size="xs" c="dimmed">{t('settings.languageNote')}</Text>
            </Stack>
            <Badge>EN</Badge>
          </Group>
        </Card>

        {/* SSH Keys */}
        <Card withBorder>
          <Group justify="space-between" mb="xs">
            <Group gap="xs">
              <IconKey size={16} />
              <Text fw={500}>SSH Keys</Text>
            </Group>
            <Button size="xs" leftSection={<IconPlus size={12} />} onClick={() => setMethodOpen(true)}>
              New
            </Button>
          </Group>
          <Text size="xs" c="dimmed" mb="md">Manage your SSH public keys</Text>

          {sshKeys.length === 0 ? (
            <Text size="sm" c="dimmed">No SSH keys yet. Click New to create or import a key.</Text>
          ) : (
            <Stack gap="sm">
              {sshKeys.map((key) => (
                <Card key={key.id} withBorder p="sm">
                  <Group justify="space-between">
                    <Group gap="sm">
                      <ThemeIcon variant="light" size="sm"><IconKey size={14} /></ThemeIcon>
                      <Stack gap={0}>
                        <Group gap="xs">
                          <Text size="sm" fw={500}>{key.name}</Text>
                          {key.type && <Badge size="xs" variant="outline" color="gray">{key.type}</Badge>}
                        </Group>
                        <Text size="xs" c="dimmed" ff="mono">{key.fingerprint}</Text>
                      </Stack>
                    </Group>
                    <Group gap="xs">
                      {key.createdAt && (
                        <Text size="xs" c="dimmed">{formatRelative(key.createdAt)}</Text>
                      )}
                      <ActionIcon
                        color="red"
                        variant="subtle"
                        size="sm"
                        onClick={() => setDeleteTarget(key)}
                      >
                        <IconTrash size={14} />
                      </ActionIcon>
                    </Group>
                  </Group>
                </Card>
              ))}
            </Stack>
          )}
        </Card>

        <Card withBorder>
          <Text fw={500} mb="md">About</Text>
          <Table fz="sm" withRowBorders={false}>
            <Table.Tbody>
              <Table.Tr>
                <Table.Td c="dimmed">Application</Table.Td>
                <Table.Td>DC Dashboard</Table.Td>
              </Table.Tr>
              <Table.Tr>
                <Table.Td c="dimmed">Version</Table.Td>
                <Table.Td><Badge variant="outline">0.1.0-alpha</Badge></Table.Td>
              </Table.Tr>
              <Table.Tr>
                <Table.Td c="dimmed">Build</Table.Td>
                <Table.Td>Vite + React 19 + TypeScript 5.9</Table.Td>
              </Table.Tr>
              <Table.Tr>
                <Table.Td c="dimmed">UI Framework</Table.Td>
                <Table.Td>Mantine 7</Table.Td>
              </Table.Tr>
            </Table.Tbody>
          </Table>
        </Card>
      </Stack>

      {/* ── Method Selection Modal ─────────────────────────────────────────── */}
      <Modal
        opened={methodOpen}
        onClose={() => setMethodOpen(false)}
        title="Add SSH Key"
        centered
        withinPortal
        zIndex={2000}
      >
        <Stack gap="md">
          <Text size="sm" c="dimmed">Choose how you want to add an SSH key.</Text>
          <Radio.Group value={selectedMode} onChange={(v) => setSelectedMode(v as 'create' | 'import')}>
            <Stack gap="sm">
              <Card withBorder style={{ cursor: 'pointer' }} onClick={() => setSelectedMode('create')}>
                <Radio
                  value="create"
                  label="Create new SSH key"
                  description="Generate a new SSH key pair via the server"
                />
              </Card>
              <Card withBorder style={{ cursor: 'pointer' }} onClick={() => setSelectedMode('import')}>
                <Radio
                  value="import"
                  label="Import existing SSH key"
                  description="Paste your existing public key for the system to store"
                />
              </Card>
            </Stack>
          </Radio.Group>
          <Group justify="flex-end" mt="xs">
            <Button variant="default" onClick={() => setMethodOpen(false)}>Cancel</Button>
            <Button onClick={handleMethodContinue}>Continue</Button>
          </Group>
        </Stack>
      </Modal>

      {/* ── Create SSH Key Modal ───────────────────────────────────────────── */}
      <Modal
        opened={createOpen}
        onClose={() => setCreateOpen(false)}
        title="Create SSH Key"
        centered
        withinPortal
        zIndex={2000}
      >
        <Stack gap="md">
          <Text size="sm" c="dimmed">A new SSH key pair will be generated by the server.</Text>
          <TextInput
            label="Name"
            placeholder="My laptop key"
            value={createName}
            onChange={(e) => setCreateName(e.target.value)}
          />
          <Group justify="flex-end" mt="xs">
            <Button variant="default" onClick={() => setCreateOpen(false)}>Cancel</Button>
            <Button onClick={handleCreate}>Create</Button>
          </Group>
        </Stack>
      </Modal>

      {/* ── Import SSH Key Modal ───────────────────────────────────────────── */}
      <Modal
        opened={importOpen}
        onClose={() => setImportOpen(false)}
        title="Import SSH Key"
        centered
        withinPortal
        zIndex={2000}
      >
        <Stack gap="md">
          <TextInput
            label="Name"
            placeholder="Workstation key"
            required
            value={importName}
            onChange={(e) => { setImportName(e.target.value); setImportNameError('') }}
            error={importNameError}
          />
          <Textarea
            label="Public Key"
            placeholder="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... user@example"
            required
            rows={4}
            ff="mono"
            value={importKey}
            onChange={(e) => { setImportKey(e.target.value); setImportKeyError('') }}
            error={importKeyError}
          />
          <Group justify="flex-end" mt="xs">
            <Button variant="default" onClick={() => setImportOpen(false)}>Cancel</Button>
            <Button loading={importLoading} onClick={() => void handleImport()}>Import</Button>
          </Group>
        </Stack>
      </Modal>

      {/* ── Delete Confirmation Modal ──────────────────────────────────────── */}
      <Modal
        opened={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        title="Delete SSH Key"
        centered
        withinPortal
        zIndex={2000}
      >
        <Stack gap="md">
          <Text size="sm">Are you sure you want to delete this SSH key?</Text>
          {deleteTarget && (
            <Card withBorder p="sm" bg="red.0">
              <Text size="sm" fw={500}>{deleteTarget.name}</Text>
              <Text size="xs" c="dimmed" ff="mono">{deleteTarget.fingerprint}</Text>
            </Card>
          )}
          <Divider />
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setDeleteTarget(null)}>Cancel</Button>
            <Button color="red" loading={deleteLoading} onClick={() => void handleDelete()}>Delete</Button>
          </Group>
        </Stack>
      </Modal>
    </>
  )
}
