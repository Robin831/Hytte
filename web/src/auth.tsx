import { createContext, useContext, useEffect, useState, useCallback, type ReactNode } from 'react'

interface User {
  id: number
  email: string
  name: string
  picture: string
  created_at: string
  is_admin: boolean
  features: Record<string, boolean>
}

interface AuthContextType {
  user: User | null
  loading: boolean
  logout: () => Promise<void>
  hasFeature: (key: string) => boolean
}

const AuthContext = createContext<AuthContextType>({
  user: null,
  loading: true,
  logout: async () => {},
  hasFeature: () => false,
})

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/auth/me')
      .then(res => res.json())
      .then(data => setUser(data.user ?? null))
      .catch(() => setUser(null))
      .finally(() => setLoading(false))
  }, [])

  const logout = useCallback(async () => {
    await fetch('/api/auth/logout', { method: 'POST' })
    setUser(null)
  }, [])

  const hasFeature = useCallback((key: string): boolean => {
    return user?.features?.[key] ?? false
  }, [user])

  return (
    <AuthContext.Provider value={{ user, loading, logout, hasFeature }}>
      {children}
    </AuthContext.Provider>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth() {
  return useContext(AuthContext)
}
