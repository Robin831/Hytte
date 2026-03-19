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

function Dashboard() {
  return (
    <div className="p-6">
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <GreetingWidget />
        <WeatherWidget />
        <DaylightWidget />
        <FitnessWidget />
        <LactateSummaryWidget />
        <ActivityFeedWidget />
        <InfraStatusWidget />
        <GitHubStatusWidget />
        <NorwegianFunWidget />
        <QuickLinksWidget />
      </div>
    </div>
  )
}

export default Dashboard
