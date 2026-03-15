import { Routes, Route, Navigate } from 'react-router-dom'
import { useAuth } from './auth'
import Sidebar from './components/Sidebar'
import ProtectedRoute from './components/ProtectedRoute'
import Home from './pages/Home'
import Dashboard from './pages/Dashboard'
import Login from './pages/Login'
import Settings from './pages/Settings'
import Weather from './pages/Weather'
import CalendarPage from './pages/CalendarPage'
import Webhooks from './pages/Webhooks'
import Notes from './pages/Notes'
import Links from './pages/Links'
import LactateTests from './pages/LactateTests'
import LactateNewTest from './pages/LactateNewTest'
import LactateTestDetail from './pages/LactateTestDetail'
import LactateInsights from './pages/LactateInsights'

function App() {
  useAuth()

  return (
    <div className="flex min-h-screen bg-gray-900 text-white">
      <Sidebar />

      <main className="flex-1 min-w-0 pt-14 md:pt-0">
        <Routes>
          {/* Public routes */}
          <Route path="/" element={<Home />} />
          <Route path="/login" element={<Login />} />
          <Route path="/weather" element={<Weather />} />
          <Route path="/calendar" element={<CalendarPage />} />
          <Route
            path="/webhooks"
            element={
              <ProtectedRoute>
                <Webhooks />
              </ProtectedRoute>
            }
          />
          <Route
            path="/notes"
            element={
              <ProtectedRoute>
                <Notes />
              </ProtectedRoute>
            }
          />

          {/* Lactate routes */}
          <Route
            path="/lactate"
            element={
              <ProtectedRoute>
                <LactateTests />
              </ProtectedRoute>
            }
          />
          <Route
            path="/lactate/new"
            element={
              <ProtectedRoute>
                <LactateNewTest />
              </ProtectedRoute>
            }
          />
          <Route
            path="/lactate/insights"
            element={
              <ProtectedRoute>
                <LactateInsights />
              </ProtectedRoute>
            }
          />
          <Route
            path="/lactate/:id"
            element={
              <ProtectedRoute>
                <LactateTestDetail />
              </ProtectedRoute>
            }
          />

          {/* Protected routes */}
          <Route
            path="/links"
            element={
              <ProtectedRoute>
                <Links />
              </ProtectedRoute>
            }
          />
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

          {/* Catch-all */}
          <Route
            path="*"
            element={
              <ProtectedRoute>
                <Navigate to="/dashboard" replace />
              </ProtectedRoute>
            }
          />
        </Routes>
      </main>
    </div>
  )
}

export default App
