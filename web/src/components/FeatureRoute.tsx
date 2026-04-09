import { Navigate } from 'react-router-dom'
import { useAuth } from '../auth'

interface FeatureRouteProps {
  feature?: string
  requireAdmin?: boolean
  /** When set, only users with this family role can access the route. */
  familyRole?: 'parent' | 'child'
  children: React.ReactNode
}

export default function FeatureRoute({ feature, requireAdmin, familyRole, children }: FeatureRouteProps) {
  const { user, loading, hasFeature, familyStatus } = useAuth()

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-2 border-gray-600 border-t-blue-500" />
      </div>
    )
  }

  if (!user) return <Navigate to="/" replace />

  if (requireAdmin && !user.is_admin) return <Navigate to="/" replace />

  if (feature && !hasFeature(feature)) return <Navigate to="/" replace />

  if (familyRole === 'parent' && familyStatus?.is_child) return <Navigate to="/" replace />
  if (familyRole === 'child' && !familyStatus?.is_child) return <Navigate to="/" replace />

  return children
}
