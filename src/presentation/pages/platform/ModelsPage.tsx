import { useEffect, useState } from 'react'
import {
  Table, Badge, Group, Text, Button, ThemeIcon, ActionIcon, Tabs,
  Modal, TextInput, NumberInput, Select, Textarea, Stack, Switch,
  Tooltip, Divider,
} from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { notifications } from '@mantine/notifications'
import {
  IconBrain, IconStar, IconStarFilled, IconPlus, IconTrash, IconServer,
  IconCloud,
} from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { AppContainer } from '@/di/container'
import type { PublicModel, LocalModel, ServerFramework } from '@/domain/platform/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'
import { EmptyState } from '@/presentation/components/EmptyState'
import { LoadingState } from '@/presentation/components/LoadingState'

const FRAMEWORK_OPTIONS: { value: ServerFramework; label: string }[] = [
  { value: 'ollama', label: 'Ollama' },
  { value: 'vllm', label: 'vLLM' },
  { value: 'sglang', label: 'SGlang' },
]

const FRAMEWORK_COLORS: Record<ServerFramework, string> = {
  ollama: 'teal',
  vllm: 'blue',
  sglang: 'violet',
}

const emptyPublicForm = {
  name: '',
  provider: '',
  accessKey: '',
  description: '',
  contextWindow: 128000,
  costPer1kTokens: 0,
  capabilities: '',
  enabled: true,
}

const emptyLocalForm = {
  name: '',
  serverHost: '',
  serverPort: 11434,
  framework: 'ollama' as ServerFramework,
  description: '',
  capabilities: '',
  enabled: true,
}

function AddPublicFormBody({
  onClose,
  onSuccess,
  addModel,
}: {
  onClose: () => void
  onSuccess: () => void
  addModel: AppContainer['platform']['addModel']
}) {
  const [form, setForm] = useState(emptyPublicForm)
  const [saving, setSaving] = useState(false)
  const handleSubmit = async () => {
    if (!form.name.trim() || !form.provider.trim() || !form.accessKey.trim()) return
    setSaving(true)
    try {
      await addModel.execute({
        type: 'public',
        name: form.name.trim(),
        provider: form.provider.trim(),
        accessKey: form.accessKey.trim() || undefined,
        description: form.description.trim(),
        contextWindow: form.contextWindow,
        costPer1kTokens: form.costPer1kTokens || undefined,
        capabilities: form.capabilities.split(',').map((s) => s.trim()).filter(Boolean),
        isDefault: false,
        enabled: form.enabled,
      })
      notifications.show({ title: 'Model added', message: form.name, color: 'green' })
      onSuccess()
    } finally {
      setSaving(false)
    }
  }
  return (
    <Stack gap="sm">
      <TextInput label="Model Name" placeholder="gpt-4o-mini" required value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} />
      <TextInput label="Provider" placeholder="OpenAI" required value={form.provider} onChange={(e) => setForm((f) => ({ ...f, provider: e.target.value }))} />
      <TextInput
        label="Access Key"
        placeholder="sk-..."
        type="password"
        required
        value={form.accessKey}
        onChange={(e) => setForm((f) => ({ ...f, accessKey: e.target.value }))}
      />
      <Textarea label="Description" placeholder="Brief description" rows={2} value={form.description} onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))} />
      <NumberInput label="Context Window (k tokens)" value={form.contextWindow} onChange={(v) => setForm((f) => ({ ...f, contextWindow: Number(v) || 0 }))} min={1} />
      <NumberInput label="Cost per 1k tokens (USD)" value={form.costPer1kTokens} onChange={(v) => setForm((f) => ({ ...f, costPer1kTokens: Number(v) || 0 }))} min={0} decimalScale={6} step={0.001} />
      <TextInput label="Capabilities" placeholder="reasoning, code (comma-separated)" value={form.capabilities} onChange={(e) => setForm((f) => ({ ...f, capabilities: e.target.value }))} />
      <Switch label="Enabled" checked={form.enabled} onChange={(e) => setForm((f) => ({ ...f, enabled: e.currentTarget.checked }))} />
      <Divider />
      <Group justify="flex-end">
        <Button variant="default" onClick={onClose}>Cancel</Button>
        <Button
          onClick={() => void handleSubmit()}
          loading={saving}
          disabled={!form.name.trim() || !form.provider.trim() || !form.accessKey.trim()}
        >
          Add Model
        </Button>
      </Group>
    </Stack>
  )
}

function AddLocalFormBody({
  onClose,
  onSuccess,
  addModel,
}: {
  onClose: () => void
  onSuccess: () => void
  addModel: AppContainer['platform']['addModel']
}) {
  const [form, setForm] = useState(emptyLocalForm)
  const [saving, setSaving] = useState(false)
  const handleSubmit = async () => {
    if (!form.name.trim() || !form.serverHost.trim()) return
    setSaving(true)
    try {
      await addModel.execute({
        type: 'local',
        name: form.name.trim(),
        serverHost: form.serverHost.trim(),
        serverPort: form.serverPort,
        framework: form.framework,
        description: form.description.trim(),
        capabilities: form.capabilities.split(',').map((s) => s.trim()).filter(Boolean),
        isDefault: false,
        enabled: form.enabled,
      })
      notifications.show({ title: 'Local model added', message: form.name, color: 'green' })
      onSuccess()
    } finally {
      setSaving(false)
    }
  }
  return (
    <Stack gap="sm">
      <TextInput label="Model Name" placeholder="llama-3.1-8b" required value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} />
      <Group grow>
        <TextInput label="Server Host / IP" placeholder="192.168.1.100" required value={form.serverHost} onChange={(e) => setForm((f) => ({ ...f, serverHost: e.target.value }))} />
        <NumberInput label="Port" value={form.serverPort} onChange={(v) => setForm((f) => ({ ...f, serverPort: Number(v) || 11434 }))} min={1} max={65535} />
      </Group>
      <Select
        label="Server Framework"
        data={FRAMEWORK_OPTIONS}
        value={form.framework}
        searchable={false}
        allowDeselect={false}
        comboboxProps={{ withinPortal: false }}
        onChange={(v) => setForm((f) => ({ ...f, framework: (v ?? 'ollama') as ServerFramework }))}
      />
      <Textarea label="Description (admin notes)" placeholder="Notes about this model" rows={3} value={form.description} onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))} />
      <TextInput label="Capabilities" placeholder="reasoning, code (comma-separated)" value={form.capabilities} onChange={(e) => setForm((f) => ({ ...f, capabilities: e.target.value }))} />
      <Switch label="Enabled" checked={form.enabled} onChange={(e) => setForm((f) => ({ ...f, enabled: e.currentTarget.checked }))} />
      <Divider />
      <Group justify="flex-end">
        <Button variant="default" onClick={onClose}>Cancel</Button>
        <Button color="teal" onClick={() => void handleSubmit()} loading={saving} disabled={!form.name.trim() || !form.serverHost.trim()}>Add Local Model</Button>
      </Group>
    </Stack>
  )
}

export function ModelsPage() {
  const { platform } = useApp()
  const [addPublicOpened, { open: openAddPublic, close: closeAddPublic }] = useDisclosure(false)
  const [addLocalOpened, { open: openAddLocal, close: closeAddLocal }] = useDisclosure(false)
  const [models, setModels] = useState<(PublicModel | LocalModel)[]>([])
  const [loading, setLoading] = useState(true)
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null)

  const load = () => {
    platform.listModels.execute().then((list) => {
      setModels(list as (PublicModel | LocalModel)[])
    }).catch(() => null).finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const publicModels = models.filter((m): m is PublicModel => m.type === 'public')
  const localModels = models.filter((m): m is LocalModel => m.type === 'local')
  const defaultModel = models.find((m) => m.isDefault)

  const handleSetDefault = async (id: string) => {
    const model = models.find((m) => m.id === id)
    if (!model) return
    await platform.setDefaultModel.execute(id)
    notifications.show({ title: 'Default model set', message: model.name, color: 'green' })
    load()
  }

  const handleDelete = async (id: string) => {
    await platform.deleteModel.execute(id)
    const model = models.find((m) => m.id === id)
    notifications.show({ title: 'Model deleted', message: model?.name ?? id, color: 'red' })
    setConfirmDeleteId(null)
    load()
  }

  if (loading) return <LoadingState />

  return (
    <>
      <PageHeader
        title={t('platform.model.titlePlural')}
        subtitle={`${models.length} models · Default: ${defaultModel?.name ?? 'none'}`}
      />

      <Tabs defaultValue="public">
        <Tabs.List mb="md">
          <Tabs.Tab value="public" leftSection={<IconCloud size={14} />}>
            Public Models
            <Badge size="xs" variant="light" ml="xs">{publicModels.length}</Badge>
          </Tabs.Tab>
          <Tabs.Tab value="local" leftSection={<IconServer size={14} />}>
            Local Models
            <Badge size="xs" variant="light" color="teal" ml="xs">{localModels.length}</Badge>
          </Tabs.Tab>
        </Tabs.List>

        {/* ── Public Models ── */}
        <Tabs.Panel value="public">
          <Group justify="flex-end" mb="sm">
            <Button size="xs" leftSection={<IconPlus size={14} />} onClick={openAddPublic}>
              Add Public Model
            </Button>
          </Group>

          {publicModels.length === 0 ? (
            <EmptyState message={t('platform.model.empty')} />
          ) : (
            <Table highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>{t('common.name')}</Table.Th>
                  <Table.Th>{t('platform.model.provider')}</Table.Th>
                  <Table.Th>{t('platform.model.contextWindow')}</Table.Th>
                  <Table.Th>{t('platform.model.cost')}</Table.Th>
                  <Table.Th>Capabilities</Table.Th>
                  <Table.Th>{t('common.enabled')}</Table.Th>
                  <Table.Th>{t('platform.model.default')}</Table.Th>
                  <Table.Th></Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {publicModels.map((model) => (
                  <Table.Tr key={model.id}>
                    <Table.Td>
                      <Group gap="xs">
                        <ThemeIcon size="sm" variant="light" color="blue"><IconBrain size={12} /></ThemeIcon>
                        <Stack gap={0}>
                          <Text size="sm" fw={500}>{model.name}</Text>
                          {model.description && <Text size="xs" c="dimmed">{model.description}</Text>}
                        </Stack>
                      </Group>
                    </Table.Td>
                    <Table.Td><Badge size="sm" variant="outline">{model.provider}</Badge></Table.Td>
                    <Table.Td><Text size="sm">{model.contextWindow.toLocaleString()}k</Text></Table.Td>
                    <Table.Td>
                      <Text size="sm">{model.costPer1kTokens != null ? `$${model.costPer1kTokens.toFixed(4)}` : '—'}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Group gap={4}>
                        {model.capabilities.slice(0, 3).map((c) => (
                          <Badge key={c} size="xs" variant="light">{c}</Badge>
                        ))}
                        {model.capabilities.length > 3 && (
                          <Text size="xs" c="dimmed">+{model.capabilities.length - 3}</Text>
                        )}
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Badge size="xs" color={model.enabled ? 'green' : 'gray'}>
                        {model.enabled ? t('common.enabled') : t('common.disabled')}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      {model.isDefault ? (
                        <Tooltip label="Default model">
                          <IconStarFilled size={16} color="var(--mantine-color-yellow-5)" />
                        </Tooltip>
                      ) : (
                        <Tooltip label="Set as default">
                          <ActionIcon variant="subtle" size="sm" onClick={() => void handleSetDefault(model.id)}>
                            <IconStar size={16} />
                          </ActionIcon>
                        </Tooltip>
                      )}
                    </Table.Td>
                    <Table.Td>
                      <Tooltip label="Delete model">
                        <ActionIcon
                          variant="subtle"
                          color="red"
                          size="sm"
                          disabled={model.isDefault}
                          onClick={() => setConfirmDeleteId(model.id)}
                        >
                          <IconTrash size={14} />
                        </ActionIcon>
                      </Tooltip>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Tabs.Panel>

        {/* ── Local Models ── */}
        <Tabs.Panel value="local">
          <Group justify="flex-end" mb="sm">
            <Button size="xs" leftSection={<IconPlus size={14} />} color="teal" onClick={openAddLocal}>
              Add Local Model
            </Button>
          </Group>

          {localModels.length === 0 ? (
            <EmptyState message="No local models configured." />
          ) : (
            <Table highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>{t('common.name')}</Table.Th>
                  <Table.Th>Server</Table.Th>
                  <Table.Th>Framework</Table.Th>
                  <Table.Th>Description</Table.Th>
                  <Table.Th>Capabilities</Table.Th>
                  <Table.Th>{t('common.enabled')}</Table.Th>
                  <Table.Th>{t('platform.model.default')}</Table.Th>
                  <Table.Th></Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {localModels.map((model) => (
                  <Table.Tr key={model.id}>
                    <Table.Td>
                      <Group gap="xs">
                        <ThemeIcon size="sm" variant="light" color="teal"><IconServer size={12} /></ThemeIcon>
                        <Text size="sm" fw={500}>{model.name}</Text>
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" ff="monospace">{model.serverHost}:{model.serverPort}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Badge size="sm" variant="light" color={FRAMEWORK_COLORS[model.framework] ?? 'gray'}>
                        {model.framework}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm" c="dimmed" truncate maw={220}>{model.description || '—'}</Text>
                    </Table.Td>
                    <Table.Td>
                      <Group gap={4}>
                        {model.capabilities.slice(0, 3).map((c) => (
                          <Badge key={c} size="xs" variant="light">{c}</Badge>
                        ))}
                        {model.capabilities.length > 3 && (
                          <Text size="xs" c="dimmed">+{model.capabilities.length - 3}</Text>
                        )}
                      </Group>
                    </Table.Td>
                    <Table.Td>
                      <Badge size="xs" color={model.enabled ? 'green' : 'gray'}>
                        {model.enabled ? t('common.enabled') : t('common.disabled')}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      {model.isDefault ? (
                        <Tooltip label="Default model">
                          <IconStarFilled size={16} color="var(--mantine-color-yellow-5)" />
                        </Tooltip>
                      ) : (
                        <Tooltip label="Set as default">
                          <ActionIcon variant="subtle" size="sm" onClick={() => void handleSetDefault(model.id)}>
                            <IconStar size={16} />
                          </ActionIcon>
                        </Tooltip>
                      )}
                    </Table.Td>
                    <Table.Td>
                      <Tooltip label="Delete model">
                        <ActionIcon
                          variant="subtle"
                          color="red"
                          size="sm"
                          disabled={model.isDefault}
                          onClick={() => setConfirmDeleteId(model.id)}
                        >
                          <IconTrash size={14} />
                        </ActionIcon>
                      </Tooltip>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          )}
        </Tabs.Panel>
      </Tabs>

      <Modal
        opened={addPublicOpened}
        onClose={closeAddPublic}
        title="Add Public Model"
        size="md"
        centered
        withinPortal
        zIndex={2000}
      >
        <AddPublicFormBody
          addModel={platform.addModel}
          onClose={closeAddPublic}
          onSuccess={() => { closeAddPublic(); load() }}
        />
      </Modal>

      <Modal
        opened={addLocalOpened}
        onClose={closeAddLocal}
        title="Add Local Model"
        size="md"
        centered
        withinPortal
        zIndex={2000}
      >
        <AddLocalFormBody
          addModel={platform.addModel}
          onClose={closeAddLocal}
          onSuccess={() => { closeAddLocal(); load() }}
        />
      </Modal>

      {/* ── Confirm Delete Modal ── */}
      <Modal
        opened={confirmDeleteId !== null}
        onClose={() => setConfirmDeleteId(null)}
        title="Delete Model"
        size="sm"
      >
        <Text size="sm" mb="md">
          Are you sure you want to delete{' '}
          <Text span fw={600}>{models.find((m) => m.id === confirmDeleteId)?.name}</Text>?
          This action cannot be undone.
        </Text>
        <Group justify="flex-end">
          <Button variant="default" onClick={() => setConfirmDeleteId(null)}>Cancel</Button>
          <Button color="red" onClick={() => confirmDeleteId && void handleDelete(confirmDeleteId)}>
            Delete
          </Button>
        </Group>
      </Modal>
    </>
  )
}
