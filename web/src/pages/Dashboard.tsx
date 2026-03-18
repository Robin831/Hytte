import GreetingWidget from '../components/widgets/GreetingWidget'
import WeatherWidget from '../components/widgets/WeatherWidget'
import DaylightWidget from '../components/widgets/DaylightWidget'
import NorwegianFunWidget from '../components/widgets/NorwegianFunWidget'

function Dashboard() {
  return (
    <div className="p-6">
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <GreetingWidget />
        <WeatherWidget />
        <DaylightWidget />
        <NorwegianFunWidget />
      </div>
    </div>
  )
}

export default Dashboard
