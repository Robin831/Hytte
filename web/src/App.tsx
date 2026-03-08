import { Routes, Route, Navigate } from 'react-router-dom'
import { useAuth } from './auth'
import ProfileDropdown from './components/ProfileDropdown'
import LoginButton from './components/LoginButton'
import ProtectedRoute from './components/ProtectedRoute'
import Home from './pages/Home'
import Dashboard from './pages/Dashboard'
import Login from './pages/Login'
import Settings from './pages/Settings'

function App() {
  const { user, loading } = useAuth()

  return (
    <div className="min-h-screen bg-gray-900 text-white">
      <header className="flex items-center justify-between px-6 py-4">
        <h2 className="text-lg font-semibold">Hytte</h2>
        <div>
          {!loading && (user ? <ProfileDropdown /> : <LoginButton />)}
        </div>
      </header>

      <Routes>
        {/* Public routes */}
        <Route path="/" element={<Home />} />
        <Route path="/login" element={<Login />} />

        {/* Protected routes — require authentication */}
        <Route
          path="/dashboard"
          element={
            <ProtectedRoute>
              <Dashboard />
            </ProtectedRoute>
          }
        />
        <Route
          path="/settings"
          element={
            <ProtectedRoute>
              <Settings />
            </ProtectedRoute>
          }
        />

        {/* Catch-all: authenticated users go to dashboard, unauthenticated are redirected to landing page by ProtectedRoute */}
        <Route
          path="*"
          element={
            <ProtectedRoute>
              <Navigate to="/dashboard" replace />
            </ProtectedRoute>
          }
        />
      </Routes>
    </div>
  )
}

export default App
