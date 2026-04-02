import { Routes, Route, Navigate } from 'react-router-dom'
import { useAuth } from './auth'
import Sidebar from './components/Sidebar'
import ProtectedRoute from './components/ProtectedRoute'
import FeatureRoute from './components/FeatureRoute'
import KioskPage from './pages/KioskPage'
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
import Chat from './pages/Chat'
import Training from './pages/Training'
import TrainingDetail from './pages/TrainingDetail'
import TrainingCompare from './pages/TrainingCompare'
import TrainingTrends from './pages/TrainingTrends'
import Infra from './pages/Infra'
import Admin from './pages/Admin'
import Family from './pages/Family'
import FamilyChildDetail from './pages/FamilyChildDetail'
import FamilyRewards from './pages/FamilyRewards'
import FamilyChallenges from './pages/family/FamilyChallenges'
import Stars from './pages/Stars'
import StarBadges from './pages/StarBadges'
import StarChallenges from './pages/StarChallenges'
import StarLeaderboard from './pages/StarLeaderboard'
import StarRewards from './pages/StarRewards'
import Transit from './pages/Transit'
import WorkHoursPage from './pages/WorkHoursPage'
import AllowancePage from './pages/AllowancePage'
import MyChoresPage from './pages/MyChoresPage'
import ForgeDashboardPage from './pages/ForgeDashboardPage'
import ForgeSettingsPage from './pages/ForgeSettingsPage'
import WordfeudPage from './pages/WordfeudPage'
import BudgetPage from './pages/BudgetPage'
import BudgetImport from './pages/BudgetImport'

function MainLayout() {
  const { user } = useAuth()

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

          {/* Feature-gated routes */}
          <Route
            path="/webhooks"
            element={
              <FeatureRoute feature="webhooks">
                <Webhooks />
              </FeatureRoute>
            }
          />
          <Route
            path="/notes"
            element={
              <FeatureRoute feature="notes">
                <Notes />
              </FeatureRoute>
            }
          />
          <Route
            path="/chat"
            element={
              <FeatureRoute feature="chat">
                <Chat />
              </FeatureRoute>
            }
          />

          {/* Lactate routes */}
          <Route
            path="/lactate"
            element={
              <FeatureRoute feature="lactate">
                <LactateTests />
              </FeatureRoute>
            }
          />
          <Route
            path="/lactate/new"
            element={
              <FeatureRoute feature="lactate">
                <LactateNewTest />
              </FeatureRoute>
            }
          />
          <Route
            path="/lactate/insights"
            element={
              <FeatureRoute feature="lactate">
                <LactateInsights />
              </FeatureRoute>
            }
          />
          <Route
            path="/lactate/:id"
            element={
              <FeatureRoute feature="lactate">
                <LactateTestDetail />
              </FeatureRoute>
            }
          />

          {/* Training routes */}
          <Route
            path="/training"
            element={
              <FeatureRoute feature="training">
                <Training />
              </FeatureRoute>
            }
          />
          <Route
            path="/training/compare"
            element={
              <FeatureRoute feature="training">
                <TrainingCompare />
              </FeatureRoute>
            }
          />
          <Route
            path="/training/trends"
            element={
              <FeatureRoute feature="training">
                <TrainingTrends />
              </FeatureRoute>
            }
          />
          <Route
            path="/training/:id"
            element={
              <FeatureRoute feature="training">
                <TrainingDetail />
              </FeatureRoute>
            }
          />

          {/* Infra route */}
          <Route
            path="/infra"
            element={
              <FeatureRoute feature="infra">
                <Infra />
              </FeatureRoute>
            }
          />

          {/* Kids Stars routes */}
          <Route
            path="/family"
            element={
              <FeatureRoute feature="kids_stars">
                <Family />
              </FeatureRoute>
            }
          />
          <Route
            path="/family/children/:id"
            element={
              <FeatureRoute feature="kids_stars" familyRole="parent">
                <FamilyChildDetail />
              </FeatureRoute>
            }
          />
          <Route
            path="/family/rewards"
            element={
              <FeatureRoute feature="kids_stars" familyRole="parent">
                <FamilyRewards />
              </FeatureRoute>
            }
          />
          <Route
            path="/family/challenges"
            element={
              <FeatureRoute feature="kids_stars" familyRole="parent">
                <FamilyChallenges />
              </FeatureRoute>
            }
          />
          <Route
            path="/stars"
            element={
              <FeatureRoute feature="kids_stars" familyRole="child">
                <Stars />
              </FeatureRoute>
            }
          />
          <Route
            path="/stars/badges"
            element={
              <FeatureRoute feature="kids_stars" familyRole="child">
                <StarBadges />
              </FeatureRoute>
            }
          />
          <Route
            path="/stars/challenges"
            element={
              <FeatureRoute feature="kids_stars" familyRole="child">
                <StarChallenges />
              </FeatureRoute>
            }
          />
          <Route
            path="/stars/leaderboard"
            element={
              <FeatureRoute feature="kids_stars" familyRole="child">
                <StarLeaderboard />
              </FeatureRoute>
            }
          />
          <Route
            path="/stars/rewards"
            element={
              <FeatureRoute feature="kids_stars" familyRole="child">
                <StarRewards />
              </FeatureRoute>
            }
          />
          <Route
            path="/links"
            element={
              <FeatureRoute feature="links">
                <Links />
              </FeatureRoute>
            }
          />

          {/* Transit route */}
          <Route
            path="/transit"
            element={
              <FeatureRoute feature="transit">
                <Transit />
              </FeatureRoute>
            }
          />

          {/* Work Hours route */}
          <Route
            path="/workhours"
            element={
              <FeatureRoute feature="work_hours">
                <WorkHoursPage />
              </FeatureRoute>
            }
          />

          {/* Budget route */}
          <Route
            path="/budget"
            element={
              <FeatureRoute feature="budget">
                <BudgetPage />
              </FeatureRoute>
            }
          />

          {/* Kids Allowance routes */}
          <Route
            path="/allowance"
            element={
              <FeatureRoute feature="kids_allowance" familyRole="parent">
                <AllowancePage />
              </FeatureRoute>
            }
          />
          <Route
            path="/chores"
            element={
              <FeatureRoute feature="kids_allowance" familyRole="child">
                <MyChoresPage />
              </FeatureRoute>
            }
          />

          {/* Wordfeud route */}
          <Route
            path="/wordfeud"
            element={
              <FeatureRoute feature="wordfeud">
                <WordfeudPage />
              </FeatureRoute>
            }
          />

          {/* Budget routes */}
          <Route
            path="/budget"
            element={
              <FeatureRoute feature="budget">
                <BudgetPage />
              </FeatureRoute>
            }
          />
          <Route
            path="/budget/import"
            element={
              <FeatureRoute feature="budget">
                <BudgetImport />
              </FeatureRoute>
            }
          />

          {/* Protected routes (accessible to all authenticated users) */}
          {/* Note: FeatureRoute also supports requireAdmin prop for admin-only routes */}
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

          {/* Forge dashboard — admin only */}
          <Route
            path="/forge"
            element={
              <FeatureRoute requireAdmin>
                <ForgeDashboardPage />
              </FeatureRoute>
            }
          />
          <Route
            path="/forge/settings"
            element={
              <FeatureRoute requireAdmin>
                <ForgeSettingsPage />
              </FeatureRoute>
            }
          />

          {/* Admin route */}
          <Route
            path="/admin"
            element={
              <ProtectedRoute>
                {user?.is_admin ? <Admin /> : <Navigate to="/dashboard" replace />}
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

function App() {
  return (
    <Routes>
      <Route path="/kiosk" element={<KioskPage />} />
      <Route path="*" element={<MainLayout />} />
    </Routes>
  )
}

export default App
