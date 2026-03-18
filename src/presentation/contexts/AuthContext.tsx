import { createContext, useContext, useEffect, useMemo, useState } from 'react'

export type UserRole = 'admin' | 'owner' | 'member'
export type UserStatus = 'active' | 'disabled'

export interface AuthUser {
  id: string
  username: string
  displayName: string
  role: UserRole
  status: UserStatus
}

interface DemoCredential extends AuthUser {
  password: string
}

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

const SESSION_KEY = 'auth-session'
const USERS_KEY = 'auth-users'

const demoCredentials: DemoCredential[] = [
  { id: 'user-admin', username: 'admin', password: 'admin', role: 'admin', displayName: 'Admin User', status: 'active' },
  { id: 'user-owner', username: 'owner', password: 'owner', role: 'owner', displayName: 'Owner User', status: 'active' },
  { id: 'user-member', username: 'member', password: 'member', role: 'member', displayName: 'Member User', status: 'active' },
]

const defaultUsers: AuthUser[] = demoCredentials.map(({ password: _password, ...u }) => u)

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

  const restoreSession = () => {
    const storedUsers = readUsersFromStorage()
    setUsers(storedUsers)

    try {
      const raw = localStorage.getItem(SESSION_KEY)
      if (!raw) {
        setCurrentUser(null)
        return
      }
      const session = JSON.parse(raw) as { username?: string }
      const username = session?.username
      if (!username) {
        setCurrentUser(null)
        return
      }
      const user = storedUsers.find((u) => u.username === username && u.status === 'active') ?? null
      setCurrentUser(user)
    } catch {
      setCurrentUser(null)
    }
  }

  useEffect(() => {
    restoreSession()
    setInitializing(false)
  }, [])

  useEffect(() => {
    if (initializing) return
    writeUsersToStorage(users)
  }, [initializing, users])

  const login = async (username: string, password: string) => {
    const matched = demoCredentials.find((u) => u.username === username && u.password === password)
    if (!matched) {
      throw new Error('Invalid username or password')
    }
    const user = users.find((u) => u.username === matched.username)
    if (!user || user.status !== 'active') {
      throw new Error('User is disabled')
    }
    localStorage.setItem(SESSION_KEY, JSON.stringify({ username: user.username }))
    setCurrentUser(user)
    return user
  }

  const logout = () => {
    localStorage.removeItem(SESSION_KEY)
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
