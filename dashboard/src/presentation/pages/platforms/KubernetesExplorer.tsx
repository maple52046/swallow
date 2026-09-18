import { useCallback, useState, type ReactNode } from 'react'
import { Badge, Button, HStack, Input, Table, Textarea } from '@chakra-ui/react'
import { RefreshCw } from 'lucide-react'
import type { Platform } from '@/domain/platform/types'
import type {
  KubernetesApplication,
  KubernetesNamespace,
  KubernetesNode,
} from '@/domain/platform/kubernetes'
import { useApp } from '@/di/AppProvider'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { LogViewer } from '@/presentation/components/LogViewer'
import { MetricGrid, SectionHeader, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { CopyButton } from '@/presentation/components/CopyButton'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useAsyncData, type AsyncData } from './useAsyncData'

/**
 * The live Kubernetes cluster explorer, rendered as Platform-detail tabs for a Swallow-deployed
 * Kubernetes Platform. Every read and write goes to the deployed cluster's own API on demand
 * and persists nothing (decision 032). The tabs are shown only when the Platform is eligible
 * (see `explorerEnabled` in PlatformDetailPage), so these components assume eligibility and
 * treat any eligibility API error as an unavailable-degrade rather than a routine state.
 */

/** Renders the standard loading/error/unavailable envelope around ready content. */
function AsyncSection<T>({ state, children }: { state: AsyncData<T>; children: (data: T) => ReactNode }) {
  if (state.status === 'loading') return <LoadingState rows={4} />
  if (state.status === 'error') return <ErrorState message={state.message} />
  if (state.status === 'unavailable') {
    return (
      <Alert status="info" title="Cluster explorer is unavailable">
        {state.message} The live cluster view appears once the platform is deployed and its credential is recorded.
      </Alert>
    )
  }
  return <>{children(state.data)}</>
}

// ---- Cluster tab: summary + nodes with cordon ----

/** Summary metrics and the node table with cordon/uncordon actions. */
export function KubernetesClusterTab({ platform, onOpenServer }: { platform: Platform; onOpenServer: (serverId: string) => void }) {
  const { platforms } = useApp()
  const summary = useAsyncData(() => platforms.kubernetes.summary(platform.id), [platform.id])
  const nodes = useAsyncData(() => platforms.kubernetes.listNodes(platform.id), [platform.id])
  const { showToast } = useToast()

  const toggleCordon = useCallback(
    async (node: KubernetesNode) => {
      try {
        await platforms.kubernetes.setNodeSchedulable(platform.id, node.name, node.unschedulable)
        showToast({ tone: 'success', title: node.unschedulable ? 'Node uncordoned' : 'Node cordoned', description: node.name })
        nodes.reload()
      } catch (error) {
        showToast({ tone: 'error', title: 'Action failed', description: error instanceof Error ? error.message : 'Could not update the node.' })
      }
    },
    [platforms, platform.id, showToast, nodes],
  )

  return (
    <div className="sw-platform-overview">
      <AsyncSection state={summary}>
        {(data) => (
          <MetricGrid
            items={[
              { label: 'Version', value: data.version || 'unknown' },
              { label: 'Nodes', value: data.nodeCount, detail: `${data.readyNodeCount} ready` },
              { label: 'Namespaces', value: data.namespaceCount },
            ]}
          />
        )}
      </AsyncSection>
      <section className="sw-section">
        <SectionHeader
          title="Nodes"
          description="Live cluster nodes correlated to Server projections."
          actions={
            <Button variant="plain" size="sm" onClick={nodes.reload}>
              <RefreshCw size={16} /> Refresh
            </Button>
          }
        />
        <AsyncSection state={nodes}>
          {(items) =>
            items.length === 0 ? (
              <EmptyState title="No nodes" message="The cluster reported no nodes." />
            ) : (
              <StickyTableFrame>
                <Table.Root size="sm" aria-label="Kubernetes nodes">
                  <Table.Header>
                    <Table.Row>
                      <Table.ColumnHeader>Node</Table.ColumnHeader>
                      <Table.ColumnHeader>Role</Table.ColumnHeader>
                      <Table.ColumnHeader>Ready</Table.ColumnHeader>
                      <Table.ColumnHeader>Scheduling</Table.ColumnHeader>
                      <Table.ColumnHeader>Server</Table.ColumnHeader>
                      <Table.ColumnHeader>Actions</Table.ColumnHeader>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {items.map((node) => (
                      <Table.Row key={node.name}>
                        <Table.Cell>
                          <span className="sw-cell-inline">
                            <strong>{node.name}</strong>
                            <CopyButton value={node.name} label="Copy node name" />
                          </span>
                        </Table.Cell>
                        <Table.Cell>
                          <StatusBadge status={node.role === 'control-plane' ? 'info' : 'neutral'} label={node.role} />
                        </Table.Cell>
                        <Table.Cell>
                          <StatusBadge status={node.ready ? 'succeeded' : 'warning'} label={node.ready ? 'Ready' : 'NotReady'} />
                        </Table.Cell>
                        <Table.Cell>
                          {node.unschedulable ? (
                            <StatusBadge status="warning" label="Cordoned" />
                          ) : (
                            <StatusBadge status="neutral" label="Schedulable" />
                          )}
                        </Table.Cell>
                        <Table.Cell>
                          {node.serverId ? (
                            <Button variant="plain" size="sm" px="0" onClick={() => onOpenServer(node.serverId!)}>
                              Open server
                            </Button>
                          ) : (
                            '-'
                          )}
                        </Table.Cell>
                        <Table.Cell>
                          <Button variant="outline" size="sm" onClick={() => void toggleCordon(node)}>
                            {node.unschedulable ? 'Uncordon' : 'Cordon'}
                          </Button>
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table.Root>
              </StickyTableFrame>
            )
          }
        </AsyncSection>
      </section>
    </div>
  )
}

// ---- Namespaces tab ----

/** Namespace list with create and delete; system namespaces are visually flagged. */
export function KubernetesNamespacesTab({ platform }: { platform: Platform }) {
  const { platforms } = useApp()
  const namespaces = useAsyncData(() => platforms.kubernetes.listNamespaces(platform.id), [platform.id])
  const { showToast } = useToast()
  const [creating, setCreating] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<KubernetesNamespace | null>(null)

  const remove = useCallback(async () => {
    if (!pendingDelete) return
    try {
      await platforms.kubernetes.deleteNamespace(platform.id, pendingDelete.name)
      showToast({ tone: 'success', title: 'Namespace deleted', description: pendingDelete.name })
      namespaces.reload()
    } catch (error) {
      showToast({ tone: 'error', title: 'Delete failed', description: error instanceof Error ? error.message : 'Could not delete the namespace.' })
    } finally {
      setPendingDelete(null)
    }
  }, [platforms, platform.id, pendingDelete, showToast, namespaces])

  return (
    <section className="sw-section">
      <SectionHeader
        title="Namespaces"
        description="Live namespaces. System namespaces are flagged and cannot be deleted."
        actions={
          <Button size="sm" colorPalette="brand" onClick={() => setCreating(true)}>
            Create namespace
          </Button>
        }
      />
      <AsyncSection state={namespaces}>
        {(items) =>
          items.length === 0 ? (
            <EmptyState title="No namespaces" message="The cluster reported no namespaces." />
          ) : (
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Kubernetes namespaces">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeader>Name</Table.ColumnHeader>
                    <Table.ColumnHeader>Phase</Table.ColumnHeader>
                    <Table.ColumnHeader>Actions</Table.ColumnHeader>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {items.map((namespace) => (
                    <Table.Row key={namespace.name}>
                      <Table.Cell>
                        <HStack gap="2">
                          <strong>{namespace.name}</strong>
                          {namespace.system && <Badge colorPalette="gray" variant="subtle">system</Badge>}
                        </HStack>
                      </Table.Cell>
                      <Table.Cell>
                        <StatusBadge status={namespace.phase === 'Active' ? 'succeeded' : 'neutral'} label={namespace.phase || 'unknown'} />
                      </Table.Cell>
                      <Table.Cell>
                        <Button
                          variant="outline"
                          size="sm"
                          colorPalette="red"
                          disabled={namespace.system}
                          onClick={() => setPendingDelete(namespace)}
                        >
                          Delete
                        </Button>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          )
        }
      </AsyncSection>

      {creating && (
        <CreateNamespaceDialog
          platformId={platform.id}
          onClose={() => setCreating(false)}
          onCreated={() => {
            setCreating(false)
            namespaces.reload()
          }}
        />
      )}
      <ConfirmDialog
        open={pendingDelete !== null}
        title="Delete namespace"
        confirmLabel="Delete namespace"
        onCancel={() => setPendingDelete(null)}
        onConfirm={() => void remove()}
      >
        Deleting <strong>{pendingDelete?.name}</strong> removes every resource inside it. This cannot be undone.
      </ConfirmDialog>
    </section>
  )
}

/** Create-namespace form dialog. */
function CreateNamespaceDialog({ platformId, onClose, onCreated }: { platformId: string; onClose: () => void; onCreated: () => void }) {
  const { platforms } = useApp()
  const { showToast } = useToast()
  const [name, setName] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const submit = async () => {
    if (!name.trim() || submitting) return
    setSubmitting(true)
    setError('')
    try {
      await platforms.kubernetes.createNamespace(platformId, name.trim())
      showToast({ tone: 'success', title: 'Namespace created', description: name.trim() })
      onCreated()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Could not create the namespace.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={() => !submitting && onClose()}
      title="Create namespace"
      description="A namespace isolates a group of workloads within the cluster."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={!name.trim() || submitting}>
            Create
          </Button>
        </>
      }
    >
      {error && (
        <Alert status="error" title="Namespace could not be created">
          {error}
        </Alert>
      )}
      <Input
        aria-label="Namespace name"
        value={name}
        onChange={(event) => setName(event.target.value)}
        placeholder="team-web"
        autoFocus
      />
    </Modal>
  )
}

// ---- Applications tab ----

/** Application list with namespace filter and per-row scale/restart/delete/logs actions. */
export function KubernetesApplicationsTab({ platform }: { platform: Platform }) {
  const { platforms } = useApp()
  const [namespace, setNamespace] = useState('')
  const applications = useAsyncData(
    () => platforms.kubernetes.listApplications(platform.id, namespace || undefined),
    [platform.id, namespace],
  )
  const { showToast } = useToast()
  const [logsFor, setLogsFor] = useState<KubernetesApplication | null>(null)
  const [applyOpen, setApplyOpen] = useState(false)

  const act = useCallback(
    async (verb: 'restart' | 'delete', app: KubernetesApplication) => {
      try {
        if (verb === 'restart') {
          await platforms.kubernetes.restartApplication(platform.id, app.namespace, app.kind, app.name)
          showToast({ tone: 'success', title: 'Restart triggered', description: `${app.kind}/${app.name}` })
        } else {
          await platforms.kubernetes.deleteApplication(platform.id, app.namespace, app.kind, app.name)
          showToast({ tone: 'success', title: 'Application deleted', description: `${app.kind}/${app.name}` })
        }
        applications.reload()
      } catch (error) {
        showToast({ tone: 'error', title: 'Action failed', description: error instanceof Error ? error.message : 'The action failed.' })
      }
    },
    [platforms, platform.id, showToast, applications],
  )

  const scale = useCallback(
    async (app: KubernetesApplication, replicas: number) => {
      try {
        await platforms.kubernetes.scaleApplication(platform.id, app.namespace, app.kind, app.name, replicas)
        showToast({ tone: 'success', title: 'Scaled', description: `${app.kind}/${app.name} to ${replicas}` })
        applications.reload()
      } catch (error) {
        showToast({ tone: 'error', title: 'Scale failed', description: error instanceof Error ? error.message : 'Could not scale.' })
      }
    },
    [platforms, platform.id, showToast, applications],
  )

  return (
    <section className="sw-section">
      <SectionHeader
        title="Applications"
        description="Deployments, DaemonSets, StatefulSets, and bare Pods aggregated from the live cluster."
        actions={
          <HStack gap="2">
            <Input
              size="sm"
              maxW="200px"
              aria-label="Filter by namespace"
              placeholder="All namespaces"
              value={namespace}
              onChange={(event) => setNamespace(event.target.value.trim())}
            />
            <Button size="sm" variant="outline" onClick={() => setApplyOpen(true)}>
              Apply YAML
            </Button>
            <Button variant="plain" size="sm" onClick={applications.reload}>
              <RefreshCw size={16} /> Refresh
            </Button>
          </HStack>
        }
      />
      <AsyncSection state={applications}>
        {(items) =>
          items.length === 0 ? (
            <EmptyState title="No applications" message="No workloads were found in the selected scope." />
          ) : (
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Kubernetes applications">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeader>Name</Table.ColumnHeader>
                    <Table.ColumnHeader>Namespace</Table.ColumnHeader>
                    <Table.ColumnHeader>Kind</Table.ColumnHeader>
                    <Table.ColumnHeader>Ready</Table.ColumnHeader>
                    <Table.ColumnHeader>Actions</Table.ColumnHeader>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {items.map((app) => (
                    <ApplicationRow
                      key={`${app.namespace}/${app.kind}/${app.name}`}
                      app={app}
                      onScale={(replicas) => void scale(app, replicas)}
                      onRestart={() => void act('restart', app)}
                      onDelete={() => void act('delete', app)}
                      onLogs={() => setLogsFor(app)}
                    />
                  ))}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          )
        }
      </AsyncSection>

      {logsFor && <PodLogsDialog platform={platform} app={logsFor} onClose={() => setLogsFor(null)} />}
      {applyOpen && (
        <ApplyManifestDialog
          platformId={platform.id}
          onClose={() => setApplyOpen(false)}
          onApplied={() => {
            setApplyOpen(false)
            applications.reload()
          }}
        />
      )}
    </section>
  )
}

/** One application row; scalable kinds expose an inline replica editor. */
function ApplicationRow({
  app,
  onScale,
  onRestart,
  onDelete,
  onLogs,
}: {
  app: KubernetesApplication
  onScale: (replicas: number) => void
  onRestart: () => void
  onDelete: () => void
  onLogs: () => void
}) {
  const scalable = app.kind === 'Deployment' || app.kind === 'StatefulSet'
  const [replicas, setReplicas] = useState(app.replicas)

  return (
    <Table.Row>
      <Table.Cell>
        <span className="sw-cell-inline">
          <strong>{app.name}</strong>
          <CopyButton value={app.name} label="Copy application name" />
        </span>
      </Table.Cell>
      <Table.Cell>{app.namespace}</Table.Cell>
      <Table.Cell>
        <StatusBadge status={app.kind === 'Pod' ? 'neutral' : 'info'} label={app.kind} />
      </Table.Cell>
      <Table.Cell>
        <StatusBadge
          status={app.readyReplicas >= app.replicas && app.replicas > 0 ? 'succeeded' : 'warning'}
          label={`${app.readyReplicas}/${app.replicas}`}
        />
      </Table.Cell>
      <Table.Cell>
        <HStack gap="2" wrap="wrap">
          {scalable && (
            <HStack gap="1">
              <Input
                size="sm"
                maxW="72px"
                type="number"
                min={0}
                aria-label={`Replicas for ${app.name}`}
                value={replicas}
                onChange={(event) => setReplicas(Math.max(0, Number(event.target.value)))}
              />
              <Button size="sm" variant="outline" onClick={() => onScale(replicas)} disabled={replicas === app.replicas}>
                Scale
              </Button>
            </HStack>
          )}
          {app.kind !== 'Pod' && (
            <Button size="sm" variant="outline" onClick={onRestart}>
              Restart
            </Button>
          )}
          <Button size="sm" variant="outline" onClick={onLogs}>
            Logs
          </Button>
          <Button size="sm" variant="outline" colorPalette="red" onClick={onDelete}>
            Delete
          </Button>
        </HStack>
      </Table.Cell>
    </Table.Row>
  )
}

/** Shows a pod's log snapshot; picks the application's first pod. */
function PodLogsDialog({ platform, app, onClose }: { platform: Platform; app: KubernetesApplication; onClose: () => void }) {
  const { platforms } = useApp()
  const logs = useAsyncData(async () => {
    // The application list carries no pods; fetch the detail to find a pod to read logs from.
    const detail = app.kind === 'Pod' ? app : await platforms.kubernetes.getApplication(platform.id, app.namespace, app.kind, app.name)
    const pod = app.kind === 'Pod' ? { name: app.name, containers: [] as string[] } : detail.pods?.[0]
    if (!pod) return { container: '', logs: 'This application has no pods to read logs from.' }
    return platforms.kubernetes.podLogs(platform.id, app.namespace, pod.name, undefined, 500)
  }, [platform.id, app.namespace, app.kind, app.name])

  return (
    <Modal
      open
      onClose={onClose}
      size="xl"
      title={`Logs — ${app.name}`}
      description="A bounded snapshot of the first pod's logs (not a live stream)."
      footer={
        <Button variant="ghost" onClick={onClose}>
          Close
        </Button>
      }
    >
      <AsyncSection state={logs}>
        {(data) => (
          <LogViewer
            text={data.logs || 'No log output.'}
            label="pod logs"
            downloadName={`swallow-${app.namespace}-${app.name}-logs`}
            height={420}
          />
        )}
      </AsyncSection>
    </Modal>
  )
}

/** Applies a YAML manifest to the cluster, with an optional dry run. */
function ApplyManifestDialog({ platformId, onClose, onApplied }: { platformId: string; onClose: () => void; onApplied: () => void }) {
  const { platforms } = useApp()
  const { showToast } = useToast()
  const [manifest, setManifest] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const run = async (dryRun: boolean) => {
    if (!manifest.trim() || submitting) return
    setSubmitting(true)
    setError('')
    try {
      const results = await platforms.kubernetes.apply(platformId, manifest, dryRun)
      const summary = results.map((result) => `${result.kind}/${result.name} ${result.action}`).join(', ')
      showToast({ tone: 'success', title: dryRun ? 'Manifest validated' : 'Manifest applied', description: summary || 'No changes.' })
      if (!dryRun) onApplied()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The manifest could not be applied.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={() => !submitting && onClose()}
      size="xl"
      title="Apply YAML manifest"
      description="Server-side applies one or more objects to the cluster. Dry run validates without persisting."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button variant="outline" onClick={() => void run(true)} loading={submitting} disabled={!manifest.trim() || submitting}>
            Dry run
          </Button>
          <Button colorPalette="brand" onClick={() => void run(false)} loading={submitting} disabled={!manifest.trim() || submitting}>
            Apply
          </Button>
        </>
      }
    >
      {error && (
        <Alert status="error" title="Manifest could not be applied">
          <span className="sw-error-detail">{error}</span>
        </Alert>
      )}
      <Textarea
        aria-label="YAML manifest"
        rows={16}
        className="mono"
        value={manifest}
        onChange={(event) => setManifest(event.target.value)}
        placeholder={'apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: nginx\n  namespace: web\n...'}
        autoFocus
      />
    </Modal>
  )
}

/** A minimal reusable confirmation dialog for destructive explorer actions. */
function ConfirmDialog({
  open,
  title,
  confirmLabel,
  onConfirm,
  onCancel,
  children,
}: {
  open: boolean
  title: string
  confirmLabel: string
  onConfirm: () => void
  onCancel: () => void
  children: React.ReactNode
}) {
  if (!open) return null
  return (
    <Modal
      open
      onClose={onCancel}
      title={title}
      role="alertdialog"
      footer={
        <>
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button colorPalette="red" onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      {children}
    </Modal>
  )
}
