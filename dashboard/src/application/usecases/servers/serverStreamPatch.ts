/**
 * Rules for patching a held Server with a live stream `upsert` (servers-stream.md), shared by the
 * Servers working set and the Server detail so both treat the stream's unresolved health the same
 * way. Pure functions over domain and port types: no React, no transport.
 */
import type { StreamedServer } from '@/application/ports/ServerEventStream'
import type { HealthAxis, Server } from '@/domain/server/types'

/**
 * Completes a streamed Server with the health its consumer last read. The stream sends
 * `status.health: null` ("not resolved in this event"), and the contract forbids reading that as a
 * state or letting it overwrite a classified one, so `known` is kept; a non-null streamed health
 * (a future stream that classifies it) wins.
 */
export function withKnownHealth(incoming: StreamedServer, known: HealthAxis): Server {
  return { ...incoming, status: { ...incoming.status, health: incoming.status.health ?? known } }
}

/**
 * Whether an upsert changed a fact that can move the Server between a monitored health state and
 * `not_applicable` — its OS Provisioning State, its lock, or its `absent` flag — so the kept health
 * may now be wrong and the Server should be re-read (GET /servers/{id}) to classify it again, for
 * example a release that makes a deployed Server `ready` and its health N/A. Changes that do not
 * touch the Server record (an exporter install finishing, a scrape failing) emit no frame at all;
 * the next complete read shows them.
 */
export function streamChangeMayMoveHealth(known: Server, incoming: StreamedServer): boolean {
  return (
    known.status.provisioning?.state !== incoming.status.provisioning?.state ||
    (known.status.provisioning?.locked ?? false) !== (incoming.status.provisioning?.locked ?? false) ||
    known.status.absent !== incoming.status.absent
  )
}
