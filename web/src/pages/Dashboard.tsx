import { useAuth } from '../auth'
import GreetingWidget from '../components/widgets/GreetingWidget'
import WeatherWidget from '../components/widgets/WeatherWidget'
import DaylightWidget from '../components/widgets/DaylightWidget'
import NorwegianFunWidget from '../components/widgets/NorwegianFunWidget'
import QuickLinksWidget from '../components/widgets/QuickLinksWidget'
import FitnessWidget from '../components/widgets/FitnessWidget'
import LactateSummaryWidget from '../components/widgets/LactateSummaryWidget'
import ActivityFeedWidget from '../components/widgets/ActivityFeedWidget'
import InfraStatusWidget from '../components/widgets/InfraStatusWidget'
import GitHubStatusWidget from '../components/widgets/GitHubStatusWidget'
import NetatmoWidget from '../components/widgets/NetatmoWidget'
import CalendarWidget from '../components/widgets/CalendarWidget'

function Dashboard() {
  const { hasFeature } = useAuth()

  return (
    <div className="p-6">
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <GreetingWidget />
        <WeatherWidget />
        <DaylightWidget />
        {hasFeature('calendar') && <CalendarWidget />}
        {hasFeature('netatmo') && <NetatmoWidget />}
        {hasFeature('training') && <FitnessWidget />}
        {hasFeature('lactate') && <LactateSummaryWidget />}
        <ActivityFeedWidget />
        {hasFeature('infra') && <InfraStatusWidget />}
        {hasFeature('infra') && <GitHubStatusWidget />}
        <NorwegianFunWidget />
        <QuickLinksWidget />
      </div>
    </div>
  )
}

export default Dashboard
