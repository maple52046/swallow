import { useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { notifications } from '@mantine/notifications'
import {
  Stepper, Button, Group, TextInput, Textarea, Select, MultiSelect, Card,
  Text, Stack, Switch, Table, ActionIcon, NumberInput,
} from '@mantine/core'
import { IconPlus, IconTrash, IconSparkles, IconArrowLeft } from '@tabler/icons-react'
import { useApp } from '@/di/AppProvider'
import type { MissionPlan, MissionPermissions, MissionStep, MissionTrigger } from '@/domain/mission/types'
import { t } from '@/presentation/app/i18n'
import { PageHeader } from '@/presentation/components/PageHeader'

const MODELS = ['gpt-4o', 'claude-3-5-sonnet', 'llama-3.1-70b', 'mistral-large', 'gemini-1.5-pro']
const PLUGINS = ['ssh', 'ipmi', 'kubectl', 'scontrol', 'nvidia-smi', 'rocm-smi', 'gpu-profiler', 'log-collector']
const GUARDRAILS = ['read-only', 'require-confirmation', 'backup-before-change', 'rollback-on-failure']

function generatePlan(goal: string, plugins: string[]): MissionPlan {
  const stepTemplates: Record<string, { name: string; description: string; action: string }[]> = {
    ssh: [{ name: 'Connect to target', description: 'Establish SSH connection', action: 'connect' }],
    'nvidia-smi': [{ name: 'Collect GPU metrics', description: 'Query nvidia-smi', action: 'query-all' }, { name: 'Check ECC errors', description: 'Query ECC counts', action: 'query-ecc' }],
    'rocm-smi': [{ name: 'Collect AMD metrics', description: 'Query rocm-smi', action: 'query-all' }],
    kubectl: [{ name: 'Get cluster state', description: 'kubectl get nodes/pods', action: 'get-nodes' }],
    scontrol: [{ name: 'Check Slurm health', description: 'sinfo/squeue', action: 'sinfo' }],
    'gpu-profiler': [{ name: 'Profile GPU workload', description: 'Collect profiling data', action: 'collect' }, { name: 'Analyze kernels', description: 'Analyze kernel performance', action: 'analyze-kernels' }],
    ipmi: [{ name: 'Collect IPMI sensors', description: 'Read sensor data', action: 'sensors' }],
    'log-collector': [{ name: 'Generate report', description: 'Create summary report', action: 'report' }],
  }

  const steps: MissionStep[] = []
  let order = 1
  for (const plugin of plugins) {
    const templates = stepTemplates[plugin] ?? [{ name: `Run ${plugin}`, description: `Execute ${plugin}`, action: 'run' }]
    for (const tmpl of templates) {
      steps.push({ id: `step-${order}`, order, ...tmpl, plugin, parameters: {} })
      order++
    }
  }

  if (goal.toLowerCase().includes('diagnos') || goal.toLowerCase().includes('analyz')) {
    steps.push({ id: `step-${order}`, order, name: 'Analyze findings', description: 'Process collected data', plugin: 'ssh', action: 'analyze', parameters: {} })
    order++
  }
  if (!steps.some((s) => s.plugin === 'log-collector')) {
    steps.push({ id: `step-${order}`, order, name: 'Generate report', description: 'Create summary report', plugin: 'log-collector', action: 'report', parameters: {} })
  }

  return { steps, estimatedDurationSeconds: steps.length * 60 }
}

export function MissionCreatePage() {
  const { missions } = useApp()
  const navigate = useNavigate()
  const [active, setActive] = useState(0)
  const [submitting, setSubmitting] = useState(false)
  const [generatingPlan, setGeneratingPlan] = useState(false)

  const [goal, setGoal] = useState('')
  const [model, setModel] = useState('gpt-4o')
  const [target, setTarget] = useState('')
  const [permissions, setPermissions] = useState<MissionPermissions>({ allowedPlugins: [], allowedTargets: [], guardrails: [] })
  const [plan, setPlan] = useState<MissionPlan>({ steps: [], estimatedDurationSeconds: 0 })
  const [trigger, setTrigger] = useState<MissionTrigger>('manual')
  const [schedule, setSchedule] = useState('')
  const [runNow, setRunNow] = useState(false)

  const handleGeneratePlan = useCallback(async () => {
    setGeneratingPlan(true)
    await new Promise((r) => setTimeout(r, 1200))
    const generated = generatePlan(goal, permissions.allowedPlugins)
    setPlan(generated)
    setGeneratingPlan(false)
    notifications.show({ title: t('mission.planGenerated'), message: `${generated.steps.length} steps generated`, color: 'green' })
  }, [goal, permissions.allowedPlugins])

  const handleSubmit = async () => {
    if (!goal || !model || !target) return
    setSubmitting(true)
    try {
      const name = goal.length > 60 ? goal.slice(0, 60) + '...' : goal
      const mission = await missions.create.execute({
        name,
        goal,
        model,
        target,
        trigger,
        schedule: trigger === 'scheduled' ? schedule : undefined,
        plan,
        permissions: { ...permissions, allowedTargets: [target, ...permissions.allowedTargets.filter((t_) => t_ !== target)] },
        tags: [],
      })
      notifications.show({ title: 'Mission created', message: mission.name, color: 'green' })
      if (runNow) {
        const run = await missions.runNow.execute(mission.id)
        navigate(`/runs/${run.id}`)
      } else {
        navigate(`/missions/${mission.id}`)
      }
    } catch (e) {
      notifications.show({ title: 'Error', message: String(e), color: 'red' })
    } finally {
      setSubmitting(false)
    }
  }

  const addStep = () => {
    const order = plan.steps.length + 1
    setPlan((p) => ({
      ...p,
      steps: [...p.steps, { id: `step-${order}`, order, name: '', description: '', plugin: '', action: '', parameters: {} }],
    }))
  }

  const removeStep = (idx: number) => {
    setPlan((p) => ({
      ...p,
      steps: p.steps.filter((_, i) => i !== idx).map((s, i) => ({ ...s, order: i + 1, id: `step-${i + 1}` })),
    }))
  }

  const updateStep = (idx: number, field: keyof MissionStep, value: string) => {
    setPlan((p) => {
      const steps = [...p.steps]
      steps[idx] = { ...steps[idx], [field]: value }
      return { ...p, steps }
    })
  }

  return (
    <>
      <Group mb="md">
        <ActionIcon variant="subtle" onClick={() => navigate('/missions')}><IconArrowLeft size={16} /></ActionIcon>
      </Group>
      <PageHeader title={t('mission.create')} subtitle="Define a new autonomous mission" />

      <Stepper active={active} onStepClick={setActive} mb="xl">
        <Stepper.Step label={t('mission.steps_goal')} description="Define the objective">
          <Card withBorder radius="md" mt="md">
            <Textarea
              label={t('mission.goal')}
              description="Describe what this mission should accomplish"
              placeholder="e.g. Run GPU health diagnostics across all hosts, check ECC errors and temperatures, generate report"
              minRows={4}
              value={goal}
              onChange={(e) => setGoal(e.target.value)}
              required
            />
          </Card>
        </Stepper.Step>

        <Stepper.Step label={t('mission.steps_model')} description="Choose AI model and target">
          <Card withBorder radius="md" mt="md">
            <Stack gap="md">
              <Select
                label={t('mission.model')}
                data={MODELS}
                value={model}
                onChange={(v) => v && setModel(v)}
                required
              />
              <TextInput
                label={t('mission.target')}
                description="Target host ID, plane ID, or pattern (e.g. host-01, plane-k8s-prod)"
                placeholder="host-01"
                value={target}
                onChange={(e) => setTarget(e.target.value)}
                required
              />
            </Stack>
          </Card>
        </Stepper.Step>

        <Stepper.Step label={t('mission.steps_permissions')} description="Set allowed plugins and guardrails">
          <Card withBorder radius="md" mt="md">
            <Stack gap="md">
              <MultiSelect
                label={t('mission.allowedPlugins')}
                data={PLUGINS}
                value={permissions.allowedPlugins}
                onChange={(v) => setPermissions((p) => ({ ...p, allowedPlugins: v }))}
              />
              <MultiSelect
                label={t('mission.guardrails')}
                data={GUARDRAILS}
                value={permissions.guardrails}
                onChange={(v) => setPermissions((p) => ({ ...p, guardrails: v }))}
              />
            </Stack>
          </Card>
        </Stepper.Step>

        <Stepper.Step label={t('mission.steps_plan')} description="Define execution steps">
          <Card withBorder radius="md" mt="md">
            <Group justify="space-between" mb="md">
              <Text fw={500}>{t('mission.plan')} ({plan.steps.length} steps)</Text>
              <Group gap="xs">
                <Button
                  variant="light" size="sm"
                  leftSection={<IconSparkles size={14} />}
                  onClick={() => void handleGeneratePlan()}
                  loading={generatingPlan}
                  disabled={!goal || permissions.allowedPlugins.length === 0}
                >
                  {t('mission.generatePlan')}
                </Button>
                <Button variant="default" size="sm" leftSection={<IconPlus size={14} />} onClick={addStep}>
                  Add Step
                </Button>
              </Group>
            </Group>
            {plan.steps.length > 0 ? (
              <Table>
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>#</Table.Th>
                    <Table.Th>{t('common.name')}</Table.Th>
                    <Table.Th>Plugin</Table.Th>
                    <Table.Th>Action</Table.Th>
                    <Table.Th></Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {plan.steps.map((step, idx) => (
                    <Table.Tr key={step.id}>
                      <Table.Td>{step.order}</Table.Td>
                      <Table.Td>
                        <TextInput
                          size="xs" placeholder="Step name" value={step.name}
                          onChange={(e) => updateStep(idx, 'name', e.target.value)}
                        />
                      </Table.Td>
                      <Table.Td>
                        <Select size="xs" placeholder="Plugin" data={PLUGINS} value={step.plugin || null}
                          onChange={(v) => updateStep(idx, 'plugin', v ?? '')} w={130} />
                      </Table.Td>
                      <Table.Td>
                        <TextInput size="xs" placeholder="action" value={step.action}
                          onChange={(e) => updateStep(idx, 'action', e.target.value)} w={100} />
                      </Table.Td>
                      <Table.Td>
                        <ActionIcon variant="subtle" color="red" size="sm" onClick={() => removeStep(idx)}>
                          <IconTrash size={12} />
                        </ActionIcon>
                      </Table.Td>
                    </Table.Tr>
                  ))}
                </Table.Tbody>
              </Table>
            ) : (
              <Text size="sm" c="dimmed" ta="center" py="md">
                No steps yet. Click "Generate Plan" or "Add Step".
              </Text>
            )}
            <Group mt="md">
              <NumberInput
                label="Estimated Duration (seconds)"
                value={plan.estimatedDurationSeconds}
                onChange={(v) => setPlan((p) => ({ ...p, estimatedDurationSeconds: Number(v) }))}
                w={200}
                size="xs"
              />
            </Group>
          </Card>
        </Stepper.Step>

        <Stepper.Step label={t('mission.steps_trigger')} description="Configure trigger and submit">
          <Card withBorder radius="md" mt="md">
            <Stack gap="md">
              <Select
                label="Trigger"
                data={[
                  { value: 'manual', label: t('mission.trigger.manual') },
                  { value: 'scheduled', label: t('mission.trigger.scheduled') },
                  { value: 'event', label: t('mission.trigger.event') },
                ]}
                value={trigger}
                onChange={(v) => v && setTrigger(v as MissionTrigger)}
              />
              {trigger === 'scheduled' && (
                <TextInput
                  label={t('mission.schedule')}
                  placeholder="0 2 * * *"
                  value={schedule}
                  onChange={(e) => setSchedule(e.target.value)}
                />
              )}
              <Switch
                label={t('mission.runNowAfterCreate')}
                checked={runNow}
                onChange={(e) => setRunNow(e.currentTarget.checked)}
              />
            </Stack>
          </Card>
        </Stepper.Step>

        <Stepper.Completed>
          <Card withBorder radius="md" mt="md">
            <Text c="dimmed">Ready to create the mission. Review your settings above.</Text>
          </Card>
        </Stepper.Completed>
      </Stepper>

      <Group justify="flex-end" mt="xl">
        {active > 0 && (
          <Button variant="default" onClick={() => setActive((a) => a - 1)}>
            {t('common.previous')}
          </Button>
        )}
        {active < 4 ? (
          <Button onClick={() => setActive((a) => a + 1)} disabled={active === 0 && !goal}>
            {t('common.next')}
          </Button>
        ) : (
          <Button onClick={() => void handleSubmit()} loading={submitting} disabled={!goal || !model || !target || plan.steps.length === 0}>
            {t('common.create')} Mission
          </Button>
        )}
      </Group>
    </>
  )
}
