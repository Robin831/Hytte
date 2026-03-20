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
import Training from './pages/Training'
import TrainingDetail from './pages/TrainingDetail'
import TrainingCompare from './pages/TrainingCompare'
import TrainingTrends from './pages/TrainingTrends'
import Infra from './pages/Infra'
import Admin from './pages/Admin'

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

          {/* Training routes */}
          <Route
            path="/training"
            element={
              <ProtectedRoute>
                <Training />
              </ProtectedRoute>
            }
          />
          <Route
            path="/training/compare"
            element={
              <ProtectedRoute>
                <TrainingCompare />
              </ProtectedRoute>
            }
          />
          <Route
            path="/training/trends"
            element={
              <ProtectedRoute>
                <TrainingTrends />
              </ProtectedRoute>
            }
          />
          <Route
            path="/training/:id"
            element={
              <ProtectedRoute>
                <TrainingDetail />
              </ProtectedRoute>
            }
          />

          {/* Infra route */}
          <Route
            path="/infra"
            element={
              <ProtectedRoute>
                <Infra />
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

          {/* Admin route */}
          <Route
            path="/admin"
            element={
              <ProtectedRoute>
                <Admin />
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
