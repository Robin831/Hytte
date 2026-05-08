import { useState, useRef } from 'react'
import { fmt } from './format'

/** Number input that displays NOK-formatted value when not focused. */
export function CurrencyInput({ value, onChange, id, step, placeholder, min }: {
  value: number
  onChange: (v: number) => void
  id: string
  step?: string
  placeholder?: string
  min?: string
}) {
  const [focused, setFocused] = useState(false)
  const [text, setText] = useState(String(value || ''))
  const ref = useRef<HTMLInputElement>(null)

  return (
    <input
      ref={ref}
      id={id}
      type={focused ? 'number' : 'text'}
      min={min ?? '0'}
      step={step ?? '1000'}
      value={focused ? text : (value ? fmt(value) : '')}
      placeholder={placeholder}
      onFocus={() => {
        setFocused(true)
        setText(String(value || ''))
      }}
      onBlur={() => {
        setFocused(false)
        const n = Number(text)
        if (!isNaN(n)) onChange(n)
      }}
      onChange={e => {
        setText(e.target.value)
        const n = Number(e.target.value)
        if (!isNaN(n)) onChange(n)
      }}
      className="w-full bg-gray-700 border border-gray-600 rounded px-3 py-2 text-sm"
    />
  )
}
