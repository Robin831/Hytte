import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import type { User } from './context.ts'
import { AuthContext } from './context.ts'

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/auth/me', { credentials: 'same-origin' })
      .then(res => {
        if (res.ok) return res.json()
        return null
      })
      .then(data => setUser(data))
      .catch(() => setUser(null))
      .finally(() => setLoading(false))
  }, [])

  const logout = async () => {
    await fetch('/api/auth/logout', {
      method: 'POST',
      credentials: 'same-origin',
    })
    setUser(null)
  }

  return (
    <AuthContext value={{ user, loading, logout }}>
      {children}
    </AuthContext>
  )
}
