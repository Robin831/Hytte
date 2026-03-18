interface WidgetProps {
  title?: string
  children: React.ReactNode
  className?: string
}

function Widget({ title, children, className = '' }: WidgetProps) {
  return (
    <div className={`bg-gray-800 rounded-xl p-6 ${className}`}>
      {title && (
        <h2 className="text-xs uppercase tracking-wide text-gray-500 mb-4">{title}</h2>
      )}
      {children}
    </div>
  )
}

export default Widget
