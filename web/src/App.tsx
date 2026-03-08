import { Routes, Route, Navigate } from 'react-router-dom'
import { useAuth } from './auth'
import ProfileDropdown from './components/ProfileDropdown'
import LoginButton from './components/LoginButton'
import ProtectedRoute from './components/ProtectedRoute'
import Sidebar from './components/Sidebar'
import Home from './pages/Home'
import Dashboard from './pages/Dashboard'
import Login from './pages/Login'
import Settings from './pages/Settings'
import Weather from './pages/Weather'
import CalendarPage from './pages/CalendarPage'
import Webhooks from './pages/Webhooks'
import Notes from './pages/Notes'
import SettingsPage from './pages/SettingsPage'

function App() {
  return (
    <div className="flex min-h-screen bg-gray-900 text-white">
      <Sidebar />

      <div className="flex-1 min-w-0">
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
          <Route path="/weather" element={<Weather />} />
          <Route path="/calendar" element={<CalendarPage />} />
          <Route path="/webhooks" element={<Webhooks />} />
          <Route path="/notes" element={<Notes />} />
          <Route path="/settings-page" element={<SettingsPage />} />

          {/* Catch-all: authenticated users go to dashboard */}
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
    </div>
  )
}

export default App
