import { useCallback, useEffect, useRef, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { ProvisionerDetail, Server } from '@/domain/server/types'
import type { ServerDockerAssignment } from './useServerDockerAssignment'

/** The server projection plus its live provisioner detail, shared by the detail tabs. */
export interface ServerDetailData {
  server: Server
  /** Live provider detail, or null when the provisioner could not be read. */
  detail: ProvisionerDetail | null
  /** Message from a failed detail read; the projection can still render without it. */
  detailError: string | null
  /** Refetches both projection and detail, e.g. after an action changes the machine. */
  reload: () => void
}

export type ServerDetailState =
  | { status: 'loading' }
  | { status: 'not-found' }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: ServerDetailData }

/**
 * Loads one server and its live provisioner detail for the detail page and its tabs.
 *
 * The projection (`getServer`) is authoritative for identity and the three axes; the
 * provisioner detail is a live proxy that may fail independently, so a detail failure
 * degrades to `detailError` rather than failing the whole page. A stale-guard drops
 * out-of-order responses. `reload` (exposed on ready data) refetches after an action.
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
        (detail) => ({ detail, detailError: null as string | null }),
        (err: Error) => ({ detail: null, detailError: err.message }),
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
        if (!cancelled) setState({ status: 'error', message: err.message })
      })
      .finally(() => {
        reloadInFlight.current = false
      })

    return () => {
      cancelled = true
    }
  }, [servers, id, nonce, reload])

  // Reflect live projection changes for this server without a manual page refresh — for example
  // an OS Image rename re-mirrored onto its deployed-image name, or a provisioning transition. The
  // stream is Site-scoped server-side, so it only subscribes once the server has loaded and its
  // Site is known; deriving siteId from the loaded server (not from every patch) keeps the
  // subscription stable across patches. onReset refetches once after a reconnect to resync any
  // events missed while disconnected.
  const siteId = state.status === 'ready' ? state.data.server.source.siteId : undefined
  useEffect(() => {
    if (!id || !siteId) return
    return serverEvents.subscribe(
      { siteId },
      {
        onEvent: (event) => {
          if (event.kind === 'removed' || event.server.id !== id) return
          setState((current) => {
            if (current.status !== 'ready' || current.data.server.id !== id) return current
            const incoming = event.server
            // Health is resolved at read time and not carried on the stream, so keep the
            // last-known value rather than blanking it until the next full reload.
            const server =
              !incoming.health && current.data.server.health
                ? { ...incoming, health: current.data.server.health }
                : incoming
            return { status: 'ready', data: { ...current.data, server } }
          })
        },
        onReset: reload,
      },
    )
  }, [serverEvents, siteId, id, reload])

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
