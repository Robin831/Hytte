import { createContext } from 'react'

export interface User {
  id: number
  email: string
  name: string
  avatarUrl: string
}

export interface AuthContextType {
  user: User | null
  loading: boolean
  logout: () => Promise<void>
}

export const AuthContext = createContext<AuthContextType>({
  user: null,
  loading: true,
  logout: async () => {},
})
