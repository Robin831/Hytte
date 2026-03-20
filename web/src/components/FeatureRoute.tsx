import { Navigate } from 'react-router-dom'
import { useAuth } from '../auth'

interface FeatureRouteProps {
  feature: string
  children: React.ReactNode
}

export default function FeatureRoute({ feature, children }: FeatureRouteProps) {
  const { user, loading, hasFeature } = useAuth()

  if (loading) return null

  if (!user) return <Navigate to="/" replace />

  if (!hasFeature(feature)) return <Navigate to="/dashboard" replace />

  return children
}
