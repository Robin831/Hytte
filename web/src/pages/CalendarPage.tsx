import { Calendar } from 'lucide-react'

export default function CalendarPage() {
  return (
    <main className="flex items-center justify-center min-h-screen">
      <div className="text-center">
        <Calendar size={48} className="mx-auto mb-4 text-gray-500" />
        <h1 className="text-2xl font-bold mb-2">Calendar</h1>
        <p className="text-gray-400">Coming soon</p>
      </div>
    </main>
  )
}
