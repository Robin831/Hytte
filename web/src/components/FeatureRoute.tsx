import { Navigate } from 'react-router-dom'
import { useAuth } from '../auth'

interface FeatureRouteProps {
  feature?: string
  requireAdmin?: boolean
  children: React.ReactNode
}

export default function FeatureRoute({ feature, requireAdmin, children }: FeatureRouteProps) {
  const { user, loading, hasFeature } = useAuth()

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-2 border-gray-600 border-t-blue-500" />
      </div>
    )
  }

  if (!user) return <Navigate to="/" replace />

  if (requireAdmin && !user.is_admin) return <Navigate to="/dashboard" replace />

  if (feature && !hasFeature(feature)) return <Navigate to="/dashboard" replace />

  return children
}
