import { useEffect, useState } from 'react'
import { Alert, AlertVariant, Button, Card, CardBody, CardTitle, Checkbox, FormGroup, FormSelect, FormSelectOption, TextArea } from '@patternfly/react-core'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/toast/toastContext'
import type { OSImage } from '@/domain/site/types'
import type { Server } from '@/domain/server/types'

/**
 * Capability input form for OS deployment. Image selection is mandatory, cloud-init is
 * optional, and ephemeral mode carries a durable warning because root changes vanish.
 */
export function DeployCard({ server, onActed }: { server: Server; onActed: () => void }) {
  const { servers, sites } = useApp()
  const { showToast } = useToast()
  const [images, setImages] = useState<OSImage[]>([])
  const [image, setImage] = useState('')
  const [userData, setUserData] = useState('')
  const [ephemeral, setEphemeral] = useState(false)
  const [busy, setBusy] = useState(false)
  useEffect(() => { let cancelled = false; sites.listOSImages(server.source.integrationId).then((items) => { if (!cancelled) setImages(items) }).catch(() => { if (!cancelled) setImages([]) }); return () => { cancelled = true } }, [server.source.integrationId, sites])
  const canDeploy = server.provisioning?.state === 'ready'
  const deploy = async () => {
    if (!image) return
    setBusy(true)
    try { const result = await servers.deployServer(server.id, { distroSeries: image, userData: userData || undefined, ephemeral: ephemeral || undefined }); showToast({ tone: 'success', title: 'Deployment accepted', description: `The provisioner reports "${result.state}"; reconciliation will follow it.` }); onActed() }
    catch (error) { showToast({ tone: 'error', title: 'Deploy failed', description: error instanceof Error ? error.message : 'Unknown error' }) }
    finally { setBusy(false) }
  }
  return <Card><CardTitle>Deploy operating system</CardTitle><CardBody><FormGroup label="Image" isRequired fieldId="deploy-image"><FormSelect id="deploy-image" value={image} onChange={(_event, value) => setImage(value)} isDisabled={!canDeploy || images.length === 0}><FormSelectOption value="" label={images.length ? 'Select an image' : 'No deployable images'} isDisabled />{images.map((item) => <FormSelectOption key={item.id} value={item.id} label={item.name} />)}</FormSelect></FormGroup><FormGroup label="Cloud-init user data" fieldId="deploy-user-data"><TextArea id="deploy-user-data" value={userData} onChange={(_event, value) => setUserData(value)} resizeOrientation="vertical" rows={4} isDisabled={!canDeploy} /></FormGroup><Checkbox id="deploy-ephemeral" label="Deploy in memory" isChecked={ephemeral} onChange={(_event, checked) => setEphemeral(checked)} isDisabled={!canDeploy} />{ephemeral && <Alert variant={AlertVariant.warning} title="Root filesystem changes will not survive reboot" isInline /> }<Button onClick={() => void deploy()} isLoading={busy} isDisabled={!canDeploy || !image || busy}>Deploy</Button>{!canDeploy && <small>Deployment requires provisioning state ready; current state is {server.provisioning?.state ?? 'unknown'}.</small>}</CardBody></Card>
}
