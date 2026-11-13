import type { Request, Route } from 'playwright/test'
import type {
  AccessAuditEntry,
  AccessSettings,
  Account,
  DirectoryStatus,
  PoolAccessPolicy,
  PoolMember,
  SudoSetting,
} from '@/domain/access/accounts'
import type { PoolRole } from '@/domain/user/types'

// Deterministic account and Pool access fixtures in the shapes of users.md, pool-access.md,
// access.md, and auth-me.md. One Pool (`pool-research`) has an owner (alice) and a member (bob); the
// signed-in identity is chosen per page, so a test can look at the console as an admin, an owner,
// or a member. State is per page, so one test's writes never leak into another. Passwords are never
// stored: the fixture only checks the documented failure cases.

const now = '2026-08-27T03:00:00Z'

/** Who the fixture signs in as: the installation admin, the owner of pool-research, or its member. */
export type FixtureIdentity = 'admin' | 'owner' | 'member'

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}

function failure(route: Route, status: number, code: string, message: string) {
  return json(route, { error: { code, message, requestId: `req-access-${code}` } }, status)
}

const accountsSeed: Account[] = [
  { id: 'admin-1', username: 'admin', role: 'admin', uidNumber: 10000, createdAt: '2026-08-01T00:00:00Z', updatedAt: '2026-08-01T00:00:00Z' },
  { id: 'user-alice', username: 'alice', role: 'user', uidNumber: 10001, createdAt: '2026-08-02T00:00:00Z', updatedAt: '2026-08-02T00:00:00Z' },
  { id: 'user-bob', username: 'bob', role: 'user', uidNumber: 10002, createdAt: '2026-08-03T00:00:00Z', updatedAt: '2026-08-03T00:00:00Z' },
]

interface StoredMembership {
  userId: string
  role: PoolRole
  sudo: SudoSetting
  sudoPasswordless: SudoSetting
}

/** Resolves one override against a Pool default the way pool-access.md describes. */
function resolve(setting: SudoSetting, poolDefault: boolean) {
  if (setting === 'inherit') return { setting, effective: poolDefault, source: 'pool' as const }
  return { setting, effective: setting === 'on', source: 'member' as const }
}

/** Creates the access fixture state of one page and the handler for its routes. */
export function createAccessFixtures(identity: FixtureIdentity = 'admin') {
  let accounts = accountsSeed.map((account) => ({ ...account }))
  let nextUid = 10003
  const policies = new Map<string, PoolAccessPolicy>([
    ['pool-research', { poolId: 'pool-research', memberSudo: false, sudoPasswordless: false, updatedAt: null, updatedBy: '' }],
  ])
  const memberships = new Map<string, StoredMembership[]>([
    ['pool-research', [
      { userId: 'user-alice', role: 'owner', sudo: 'inherit', sudoPasswordless: 'inherit' },
      { userId: 'user-bob', role: 'member', sudo: 'inherit', sudoPasswordless: 'inherit' },
    ]],
  ])
  let settings: AccessSettings = { adminSudoPasswordless: true, posixIdStart: 10000, updatedAt: null, updatedBy: '' }
  const audit: AccessAuditEntry[] = [
    { id: 'audit-2', at: '2026-08-26T09:00:00Z', actor: 'alice', action: 'member.added', target: 'bob', poolId: 'pool-research', detail: 'role member' },
    { id: 'audit-1', at: '2026-08-26T08:00:00Z', actor: 'admin', action: 'user.created', target: 'bob', poolId: '', detail: 'role user' },
  ]
  const directory: DirectoryStatus = {
    configured: true,
    hostUrl: 'ldaps://swallow.lab.example:636',
    baseDn: 'dc=swallow,dc=internal',
    conditions: [
      { type: 'realized', status: 'true', reason: 'converged', message: '', lastTransitionTime: now, observedGeneration: 0 },
      { type: 'requires_attention', status: 'false', reason: 'attention_not_needed', message: '', lastTransitionTime: now, observedGeneration: 0 },
    ],
    lastAppliedAt: now,
    observedAt: now,
    users: 3,
    groups: 4,
    sudoRules: 3,
    hosts: 5,
  }

  const caller = identity === 'admin' ? accounts[0] : identity === 'owner' ? accounts[1] : accounts[2]
  const record = (entry: Omit<AccessAuditEntry, 'id' | 'at' | 'actor'>) => {
    audit.unshift({ id: `audit-${audit.length + 1}`, at: now, actor: caller.username, ...entry })
  }

  const memberView = (poolId: string, stored: StoredMembership): PoolMember => {
    const policy = policies.get(poolId)!
    const username = accounts.find((account) => account.id === stored.userId)?.username ?? stored.userId
    return {
      userId: stored.userId,
      username,
      poolId,
      role: stored.role,
      sudo: stored.role === 'owner' ? { setting: stored.sudo, effective: true, source: 'role' } : resolve(stored.sudo, policy.memberSudo),
      sudoPasswordless: resolve(stored.sudoPasswordless, policy.sudoPasswordless),
      createdAt: '2026-08-26T09:00:00Z',
      updatedAt: now,
    }
  }

  const poolRoleOf = (poolId: string): PoolRole | null =>
    memberships.get(poolId)?.find((membership) => membership.userId === caller.id)?.role ?? null
  const readsPool = (poolId: string) => caller.role === 'admin' || poolRoleOf(poolId) !== null
  const operatesPool = (poolId: string) => caller.role === 'admin' || poolRoleOf(poolId) === 'owner'

  /** The auth-me body of the signed-in identity, with its memberships. */
  const me = () => ({
    id: caller.id,
    username: caller.username,
    role: caller.role,
    authMethod: 'session',
    memberships: [...memberships.entries()].flatMap(([poolId, items]) =>
      items.filter((item) => item.userId === caller.id).map((item) => ({ poolId, poolName: 'research', siteId: 'site-a', role: item.role }))),
  })

  /** Answers a request when it belongs to the access contracts; returns `false` otherwise. */
  async function handle(route: Route, request: Request, url: URL): Promise<boolean> {
    const path = url.pathname
    const method = request.method()
    if (path === '/api/v1/auth/me' && method === 'GET') {
      await json(route, me())
      return true
    }
    if (path === '/api/v1/auth/me/password' && method === 'PUT') {
      const body = request.postDataJSON() as { currentPassword?: string } | null
      if (body?.currentPassword === 'wrong-password') {
        await failure(route, 400, 'validation_error', 'The current password is wrong.')
      } else {
        await route.fulfill({ status: 204 })
      }
      return true
    }

    const adminOnly = path === '/api/v1/users' || path.startsWith('/api/v1/users/') || path.startsWith('/api/v1/access/')
    if (adminOnly && caller.role !== 'admin') {
      await failure(route, 403, 'forbidden', 'This action requires the admin role.')
      return true
    }
    if (path === '/api/v1/users' && method === 'GET') {
      await json(route, { items: [...accounts].sort((a, b) => a.username.localeCompare(b.username)) })
      return true
    }
    if (path === '/api/v1/users' && method === 'POST') {
      const body = request.postDataJSON() as { username: string; role: Account['role'] }
      if (accounts.some((account) => account.username === body.username)) {
        await failure(route, 409, 'conflict', `The username ${body.username} is taken.`)
        return true
      }
      if (body.username === 'root') {
        await failure(route, 400, 'validation_error', 'The username root is reserved.')
        return true
      }
      const account: Account = { id: `user-${body.username}`, username: body.username, role: body.role, uidNumber: nextUid++, createdAt: now, updatedAt: now }
      accounts = [...accounts, account]
      record({ action: 'user.created', target: account.username, poolId: '', detail: `role ${account.role}` })
      await json(route, account, 201)
      return true
    }
    const userMatch = path.match(/^\/api\/v1\/users\/([^/]+)$/)
    if (userMatch) {
      const id = decodeURIComponent(userMatch[1])
      const account = accounts.find((item) => item.id === id)
      if (!account) {
        await failure(route, 404, 'not_found', 'User not found.')
        return true
      }
      if (method === 'PATCH') {
        const body = request.postDataJSON() as { role?: Account['role'] }
        const admins = accounts.filter((item) => item.role === 'admin')
        if (body.role === 'user' && account.role === 'admin' && admins.length === 1) {
          await failure(route, 409, 'conflict', 'The last admin cannot lose the admin role.')
          return true
        }
        const updated = { ...account, role: body.role ?? account.role, updatedAt: now }
        accounts = accounts.map((item) => (item.id === id ? updated : item))
        record({ action: 'user.updated', target: account.username, poolId: '', detail: 'changed' })
        await json(route, updated)
        return true
      }
      if (method === 'DELETE') {
        accounts = accounts.filter((item) => item.id !== id)
        for (const items of memberships.values()) {
          const index = items.findIndex((item) => item.userId === id)
          if (index >= 0) items.splice(index, 1)
        }
        record({ action: 'user.deleted', target: account.username, poolId: '', detail: '' })
        await route.fulfill({ status: 204 })
        return true
      }
    }
    if (path === '/api/v1/access/settings' && method === 'GET') {
      await json(route, settings)
      return true
    }
    if (path === '/api/v1/access/settings' && method === 'PUT') {
      const body = request.postDataJSON() as { adminSudoPasswordless: boolean }
      settings = { ...settings, adminSudoPasswordless: body.adminSudoPasswordless, updatedAt: now, updatedBy: caller.username }
      record({ action: 'settings.updated', target: 'settings', poolId: '', detail: `adminSudoPasswordless ${body.adminSudoPasswordless}` })
      await json(route, settings)
      return true
    }
    if (path === '/api/v1/access/audit' && method === 'GET') {
      await json(route, { items: audit })
      return true
    }
    if (path === '/api/v1/access/directory' && method === 'GET') {
      await json(route, directory)
      return true
    }
    if (path === '/api/v1/access/directory/sync' && method === 'POST') {
      await route.fulfill({ status: 202 })
      return true
    }

    const poolMatch = path.match(/^\/api\/v1\/infrastructure\/pools\/([^/]+)\/(members|access-policy)(?:\/([^/]+))?$/)
    if (!poolMatch) return false
    const poolId = decodeURIComponent(poolMatch[1])
    const items = memberships.get(poolId)
    if (!items || !readsPool(poolId)) {
      await failure(route, 404, 'not_found', 'Pool not found.')
      return true
    }
    if (method !== 'GET' && !operatesPool(poolId)) {
      await failure(route, 403, 'forbidden', 'Only an owner of the Pool or an admin can change its access.')
      return true
    }
    if (poolMatch[2] === 'access-policy') {
      if (method === 'PUT') {
        const body = request.postDataJSON() as { memberSudo: boolean; sudoPasswordless: boolean }
        policies.set(poolId, { poolId, ...body, updatedAt: now, updatedBy: caller.username })
        record({ action: 'pool_policy.updated', target: 'research', poolId, detail: '' })
      }
      await json(route, policies.get(poolId))
      return true
    }
    const userId = poolMatch[3] ? decodeURIComponent(poolMatch[3]) : null
    if (!userId && method === 'GET') {
      const views = items.map((item) => memberView(poolId, item)).sort((a, b) => a.username.localeCompare(b.username))
      await json(route, { items: views })
      return true
    }
    if (!userId && method === 'POST') {
      const body = request.postDataJSON() as { username: string; role: PoolRole; sudo?: SudoSetting; sudoPasswordless?: SudoSetting }
      const account = accounts.find((item) => item.username === body.username)
      if (!account) {
        await failure(route, 404, 'not_found', `No user is named ${body.username}.`)
        return true
      }
      if (items.some((item) => item.userId === account.id)) {
        await failure(route, 409, 'conflict', `${body.username} is already a member.`)
        return true
      }
      const stored: StoredMembership = { userId: account.id, role: body.role, sudo: body.sudo ?? 'inherit', sudoPasswordless: body.sudoPasswordless ?? 'inherit' }
      items.push(stored)
      record({ action: 'member.added', target: account.username, poolId, detail: `role ${body.role}` })
      await json(route, memberView(poolId, stored), 201)
      return true
    }
    const stored = items.find((item) => item.userId === userId)
    if (!stored) {
      await failure(route, 404, 'not_found', 'Membership not found.')
      return true
    }
    if (method === 'PATCH') {
      const body = request.postDataJSON() as Partial<Omit<StoredMembership, 'userId'>>
      Object.assign(stored, body)
      record({ action: 'member.updated', target: userId!, poolId, detail: '' })
      await json(route, memberView(poolId, stored))
      return true
    }
    if (method === 'DELETE') {
      items.splice(items.indexOf(stored), 1)
      record({ action: 'member.removed', target: userId!, poolId, detail: '' })
      await route.fulfill({ status: 204 })
      return true
    }
    return false
  }

  return { handle }
}
