import { useCallback, useEffect, useRef, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { streamChangeMayMoveHealth, withKnownHealth } from '@/application/usecases/servers/serverStreamPatch'
import type { ProvisionerDetail, Server } from '@/domain/server/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import type { ServerDockerAssignment } from './useServerDockerAssignment'
import { useServerReread } from './useServerReread'

/** The server projection plus its live provisioner detail, shared by the detail tabs. */
export interface ServerDetailData {
  server: Server
  /** Live provider detail, or null when the provisioner could not be read. */
  detail: ProvisionerDetail | null
  /** A failed detail read, with its request ID; the projection can still render without it. */
  detailError: DisplayedFailure | null
  /** Refetches both projection and detail, e.g. after an action changes the machine. */
  reload: () => void
}

/**
 * The detail route's read of one Server: `not-found` for an unknown id, `error` for a failed
 * projection read with its message and request ID, and `ready` once the projection loaded.
 */
export type ServerDetailState =
  | { status: 'loading' }
  | { status: 'not-found' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; data: ServerDetailData }

/**
 * Loads one server and its live provisioner detail for the detail page and its tabs.
 *
 * The projection (`getServer`) is authoritative for identity and the three axes; the
 * provisioner detail is a live proxy that may fail independently, so a detail failure
 * degrades to `detailError` rather than failing the whole page. A stale-guard drops
 * out-of-order responses. `reload` (exposed on ready data) refetches after an action.
 *
 * Once loaded, it patches the projection from the Site's live stream and, when a patch can change
 * the Server's health, re-reads just the projection (`useServerReread`); both stop when the id
 * changes or the page unmounts.
 */
export function useServerDetail(id: string | undefined): ServerDetailState {
  const { servers, serverEvents } = useApp()
  // Seed from id so the no-id case needs no synchronous setState inside the effect: a
  // detail route with no id is a not-found from the first render.
  const [state, setState] = useState<ServerDetailState>(
    id ? { status: 'loading' } : { status: 'not-found' },
  )
  const [nonce, setNonce] = useState(0)
  const reloadInFlight = useRef(false)

  const reload = useCallback(() => {
    if (reloadInFlight.current) return
    reloadInFlight.current = true
    setNonce((value) => value + 1)
  }, [])

  useEffect(() => {
    if (!id) return

    let cancelled = false

    // The two reads are independent: detail failing must not hide the projection.
    Promise.all([
      servers.getServer(id),
      servers.getProvisionerDetail(id).then(
        (detail) => ({ detail, detailError: null as DisplayedFailure | null }),
        (err: Error) => ({ detail: null, detailError: failureOf(err, 'The provisioner detail could not be read.') }),
      ),
    ])
      .then(([server, detailResult]) => {
        if (cancelled) return
        if (server === null) {
          setState({ status: 'not-found' })
          return
        }
        setState({
          status: 'ready',
          data: {
            server,
            detail: detailResult.detail,
            detailError: detailResult.detailError,
            reload,
          },
        })
      })
      .catch((err: Error) => {
        if (!cancelled) setState({ status: 'error', ...failureOf(err, 'The Server could not be loaded.') })
      })
      .finally(() => {
        reloadInFlight.current = false
      })

    return () => {
      cancelled = true
    }
  }, [servers, id, nonce, reload])

  // The latest state, read by the stream callback (never during render) to compare an upsert with
  // the Server shown before its patch is applied.
  const stateRef = useRef(state)
  useEffect(() => {
    stateRef.current = state
  })

  // A health re-read replaces only the projection; the provisioner detail is a live proxy that a
  // health change does not affect, so it is not fetched again. A failed or `null` answer (the
  // Server was deleted meanwhile) keeps what the page shows, like an ignored `removed` frame.
  const { reread, isRereading } = useServerReread(id ?? '', {
    onRead: (_, server) => {
      if (!server) return
      setState((current) => (
        current.status === 'ready' && current.data.server.id === server.id
          ? { status: 'ready', data: { ...current.data, server } }
          : current
      ))
    },
  })

  // Reflect live projection changes for this server without a manual page refresh — for example
  // an OS Image rename re-mirrored onto its deployed-image name, or a provisioning transition. The
  // stream is Site-scoped server-side, so it only subscribes once the server has loaded and its
  // Site is known; deriving siteId from the loaded server (not from every patch) keeps the
  // subscription stable across patches. onReset refetches once after a reconnect to resync any
  // events missed while disconnected.
  //
  // The stream never classifies health, so a patch keeps the health last read; when the upsert
  // changes the provisioning state, lock, or `absent` flag, the Server is re-read so its health
  // (for example N/A after a release) updates in place.
  const siteId = state.status === 'ready' ? state.data.server.status.source.siteId : undefined
  useEffect(() => {
    if (!id || !siteId) return
    return serverEvents.subscribe(
      { siteId },
      {
        onEvent: (event) => {
          if (event.kind === 'removed' || event.server.id !== id) return
          const shown = stateRef.current
          if (shown.status !== 'ready' || shown.data.server.id !== id) return
          const incoming = event.server
          setState((current) => {
            if (current.status !== 'ready' || current.data.server.id !== id) return current
            const server = withKnownHealth(incoming, current.data.server.status.health)
            return { status: 'ready', data: { ...current.data, server } }
          })
          // An unfinished re-read is restarted too, so the answer that lands postdates this event.
          if (streamChangeMayMoveHealth(shown.data.server, incoming) || isRereading(id)) reread(id)
        },
        onReset: reload,
      },
    )
  }, [serverEvents, siteId, id, reload, reread, isRereading])

  return state
}

/**
 * What the detail page shares with its tab routes: the loaded detail plus the Server's Docker CE
 * assignment, which the page also needs to decide whether the Containers tab is listed.
 */
export interface ServerDetailContext extends ServerDetailData {
  docker: ServerDockerAssignment
}

/**
 * Reads the loaded server detail from the router outlet.
 *
 * The parent detail page loads once and shares the data with its tab routes through the
 * outlet context, so a tab never refetches. Only valid inside a tab rendered under
 * `ServerDetailPage`.
 */
export function useServerDetailContext(): ServerDetailContext {
  return useOutletContext<ServerDetailContext>()
}
