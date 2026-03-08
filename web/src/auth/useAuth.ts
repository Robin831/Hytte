import { useContext } from 'react'
import { AuthContext } from './context.ts'

export function useAuth() {
  return useContext(AuthContext)
}
