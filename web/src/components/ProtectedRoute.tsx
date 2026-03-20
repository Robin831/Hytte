import { Navigate } from 'react-router-dom'
import { useAuth } from '../auth'

export default function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth()

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-2 border-gray-600 border-t-blue-500" />
      </div>
    )
  }

  if (!user) return <Navigate to="/" replace />

  return children
}
