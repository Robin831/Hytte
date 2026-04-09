import { Navigate } from 'react-router-dom'
import { useAuth } from '../auth'

function Login() {
  const { user, loading } = useAuth()

  if (loading) return null

  if (user) return <Navigate to="/" replace />

  // Sign-in button is in the header — redirect to home
  return <Navigate to="/" replace />
}

export default Login
