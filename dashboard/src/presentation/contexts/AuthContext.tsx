/* eslint-disable react-refresh/only-export-components */
import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { User, UserRole, UserStatus } from '@/domain/user/types'

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
const defaultUsers: AuthUser[] = [
  { id: 'user-admin', username: 'admin', role: 'admin', displayName: 'Admin User', status: 'active' },
  { id: 'user-owner', username: 'owner', role: 'owner', displayName: 'Owner User', status: 'active' },
  { id: 'user-member', username: 'member', role: 'member', displayName: 'Member User', status: 'active' },
]
const AuthContext = createContext<AuthContextValue | null>(null)

function readUsersFromStorage(): AuthUser[] {
  try {
    const parsed = JSON.parse(localStorage.getItem(USERS_KEY) ?? '[]') as AuthUser[]
    return Array.isArray(parsed) && parsed.length ? parsed : defaultUsers
  } catch { return defaultUsers }
}

function writeUsersToStorage(users: AuthUser[]): void {
  try { localStorage.setItem(USERS_KEY, JSON.stringify(users)) } catch {
    // Browser storage policy may disable the demo user-list preference.
  }
}

/** Session provider that consumes only the application auth port. */
export function AuthProvider({ children }: { children: React.ReactNode }) {
  const { auth } = useApp()
  const [users, setUsers] = useState<AuthUser[]>(defaultUsers)
  const [currentUser, setCurrentUser] = useState<AuthUser | null>(null)
  const [initializing, setInitializing] = useState(true)

  const restoreSession = useCallback(async () => {
    setUsers(readUsersFromStorage())
    if (!auth.hasSession()) { setCurrentUser(null); return }
    try { setCurrentUser(await auth.restoreSession()) }
    catch { auth.logout(); setCurrentUser(null) }
  }, [auth])

  useEffect(() => {
    let cancelled = false
    const initialize = async () => { await restoreSession(); if (!cancelled) setInitializing(false) }
    void initialize()
    return () => { cancelled = true }
  }, [restoreSession])

  useEffect(() => { if (!initializing) writeUsersToStorage(users) }, [initializing, users])

  const login = async (username: string, password: string): Promise<AuthUser> => {
    const user = await auth.login(username, password)
    setCurrentUser(user)
    return user
  }
  const logout = () => { auth.logout(); setCurrentUser(null) }
  const updateUserRole = (username: string, role: UserRole) => {
    setUsers((current) => {
      const next = current.map((user) => user.username === username ? { ...user, role } : user)
      if (currentUser?.username === username) setCurrentUser(next.find((user) => user.username === username) ?? null)
      return next
    })
  }
  const value: AuthContextValue = { currentUser, isAuthenticated: currentUser !== null, initializing, users, login, logout, restoreSession: () => { void restoreSession() }, updateUserRole }
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

/** Returns the current authenticated session boundary. */
export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext)
  if (!context) throw new Error('useAuth must be used within AuthProvider')
  return context
}
