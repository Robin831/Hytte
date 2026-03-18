import GreetingWidget from '../components/widgets/GreetingWidget'

function Dashboard() {
  return (
    <div className="p-6">
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <GreetingWidget />
      </div>
    </div>
  )
}

export default Dashboard
