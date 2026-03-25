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

export interface FamilyStatus {
  is_parent: boolean
  is_child: boolean
}

interface AuthContextType {
  user: User | null
  loading: boolean
  logout: () => Promise<void>
  hasFeature: (key: string) => boolean
  familyStatus: FamilyStatus | null
  refreshFamilyStatus: () => Promise<void>
}

const AuthContext = createContext<AuthContextType>({
  user: null,
  loading: true,
  logout: async () => {},
  hasFeature: () => false,
  familyStatus: null,
  refreshFamilyStatus: async () => {},
})

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [familyStatus, setFamilyStatus] = useState<FamilyStatus | null>(null)

  const fetchFamilyStatus = useCallback(async (currentUser: User | null) => {
    if (!currentUser || !currentUser.features?.['kids_stars']) {
      setFamilyStatus(null)
      return
    }
    try {
      const res = await fetch('/api/family/status', { credentials: 'include' })
      if (res.ok) {
        const data = await res.json()
        setFamilyStatus({ is_parent: data.is_parent, is_child: data.is_child })
      }
    } catch {
      // non-fatal — leave familyStatus null
    }
  }, [])

  useEffect(() => {
    fetch('/api/auth/me', { credentials: 'include' })
      .then(res => res.json())
      .then(data => {
        if (data.user) {
          const u: User = { ...data.user, features: data.features ?? {} }
          setUser(u)
          return fetchFamilyStatus(u)
        } else {
          setUser(null)
        }
      })
      .catch(() => setUser(null))
      .finally(() => setLoading(false))
  }, [fetchFamilyStatus])

  const logout = useCallback(async () => {
    await fetch('/api/auth/logout', { method: 'POST' })
    setUser(null)
    setFamilyStatus(null)
  }, [])

  const hasFeature = useCallback((key: string): boolean => {
    return user?.features?.[key] ?? false
  }, [user])

  const refreshFamilyStatus = useCallback(async () => {
    await fetchFamilyStatus(user)
  }, [user, fetchFamilyStatus])

  return (
    <AuthContext.Provider value={{ user, loading, logout, hasFeature, familyStatus, refreshFamilyStatus }}>
      {children}
    </AuthContext.Provider>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth() {
  return useContext(AuthContext)
}
