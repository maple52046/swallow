import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import type { User, UserRole, UserStatus } from '@/domain/user/types'
import * as authApi from '@/infrastructure/api/authApi'
import { tokenStore, ApiRequestError } from '@/infrastructure/api/client'
import type { MeResponse } from '@/infrastructure/api/types'

// Backend role 'user' maps to frontend UserRole 'member'.
function mapBackendRole(role: MeResponse['role']): UserRole {
  return role === 'user' ? 'member' : role
}

export type { UserRole, UserStatus }
export type AuthUser = User

interface AuthContextValue {
  currentUser: AuthUser | null
  isAuthenticated: boolean
  initializing: boolean
  users: AuthUser[]
  login: (username: string, password: string) => Promise<AuthUser>
  logout: () => void
  restoreSession: () => void
  updateUserRole: (username: string, role: UserRole) => void
}

const USERS_KEY = 'auth-users'

// Demo users list for display purposes (user management UI).
// This is distinct from authentication — auth is handled by the real backend.
const defaultUsers: AuthUser[] = [
  { id: 'user-admin', username: 'admin', role: 'admin', displayName: 'Admin User', status: 'active' },
  { id: 'user-owner', username: 'owner', role: 'owner', displayName: 'Owner User', status: 'active' },
  { id: 'user-member', username: 'member', role: 'member', displayName: 'Member User', status: 'active' },
]

const AuthContext = createContext<AuthContextValue | null>(null)

function readUsersFromStorage(): AuthUser[] {
  try {
    const raw = localStorage.getItem(USERS_KEY)
    if (!raw) return defaultUsers
    const parsed = JSON.parse(raw) as AuthUser[]
    if (!Array.isArray(parsed) || parsed.length === 0) return defaultUsers
    return parsed
  } catch {
    return defaultUsers
  }
}

function writeUsersToStorage(users: AuthUser[]) {
  localStorage.setItem(USERS_KEY, JSON.stringify(users))
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [users, setUsers] = useState<AuthUser[]>(defaultUsers)
  const [currentUser, setCurrentUser] = useState<AuthUser | null>(null)
  const [initializing, setInitializing] = useState(true)

  // Awaited before initializing is cleared. Returning early here would let the route
  // guard decide the user is signed out while /auth/me is still in flight, which shows
  // the login page for a moment on every reload of a protected page.
  const restoreSession = async () => {
    const storedUsers = readUsersFromStorage()
    setUsers(storedUsers)

    const token = tokenStore.get()
    if (!token) {
      setCurrentUser(null)
      return
    }

    // Verify the stored token is still valid by calling /auth/me.
    try {
      const me = await authApi.getMe()
      setCurrentUser({
        id: me.id,
        username: me.username,
        // TODO(api): Backend does not provide displayName yet. Fall back to username.
        displayName: me.username,
        role: mapBackendRole(me.role),
        status: 'active',
      })
    } catch {
      // Token is invalid or expired — clear it and require re-login.
      tokenStore.clear()
      setCurrentUser(null)
    }
  }

  useEffect(() => {
    void restoreSession().finally(() => setInitializing(false))
  }, [])

  useEffect(() => {
    if (initializing) return
    writeUsersToStorage(users)
  }, [initializing, users])

  const login = async (username: string, password: string): Promise<AuthUser> => {
    try {
      // Call the real backend — token is stored inside authApi.login().
      await authApi.login(username, password)
      const me = await authApi.getMe()
      const user: AuthUser = {
        id: me.id,
        username: me.username,
        // TODO(api): Backend does not provide displayName yet. Fall back to username.
        displayName: me.username,
        role: mapBackendRole(me.role),
        status: 'active',
      }
      setCurrentUser(user)
      return user
    } catch (err) {
      if (err instanceof ApiRequestError) {
        if (err.status === 401) throw new Error('Invalid username or password')
        throw new Error(err.message)
      }
      throw err
    }
  }

  const logout = () => {
    authApi.logout()
    setCurrentUser(null)
  }

  const updateUserRole = (username: string, role: UserRole) => {
    setUsers((prev) => {
      const next = prev.map((u) => (u.username === username ? { ...u, role } : u))
      const updatedCurrent = currentUser ? next.find((u) => u.username === currentUser.username) ?? null : null
      setCurrentUser(updatedCurrent)
      return next
    })
  }

  const value = useMemo<AuthContextValue>(() => ({
    currentUser,
    isAuthenticated: currentUser !== null,
    initializing,
    users,
    login,
    logout,
    restoreSession,
    updateUserRole,
  }), [currentUser, initializing, users])

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth must be used within AuthProvider')
  }
  return ctx
}
