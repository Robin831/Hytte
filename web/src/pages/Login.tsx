import { Navigate } from 'react-router'
import { useAuth } from '../auth'

function Login() {
  const { user, loading } = useAuth()

  if (loading) return null

  if (user) return <Navigate to="/dashboard" replace />

  // Sign-in button is in the header — redirect to home
  return <Navigate to="/" replace />
}

export default Login
