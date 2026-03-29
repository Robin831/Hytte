import React, { useEffect, useRef, useState } from 'react'
import { cn } from '../../lib/utils'

// All 15-minute increments across a 24-hour day (96 options)
const TIME_OPTIONS: string[] = []
for (let h = 0; h < 24; h++) {
  for (let m = 0; m < 60; m += 15) {
    TIME_OPTIONS.push(`${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`)
  }
}

/**
 * Parse a loosely-typed time string to HH:MM.
 * Accepts: "0600" → "06:00", "630" → "06:30", "9" → "09:00",
 *          "06:30" → "06:30", "9:5" → "09:05"
 */
function parseTimeInput(raw: string): string | null {
  const trimmed = raw.trim()
  if (!trimmed) return null

  // Already looks like HH:MM — validate and return
  const colonMatch = trimmed.match(/^(\d{1,2}):(\d{2})$/)
  if (colonMatch) {
    const h = parseInt(colonMatch[1], 10)
    const m = parseInt(colonMatch[2], 10)
    if (h >= 0 && h <= 23 && m >= 0 && m <= 59) {
      return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`
    }
    return null
  }

  // Digits only
  const digits = trimmed.replace(/\D/g, '')
  if (digits.length === 0) return null

  let h: number, m: number

  if (digits.length <= 2) {
    // "9" → 09:00, "09" → 09:00
    h = parseInt(digits, 10)
    m = 0
  } else if (digits.length === 3) {
    // "630" → 6:30, "930" → 9:30
    h = parseInt(digits[0], 10)
    m = parseInt(digits.slice(1), 10)
  } else {
    // "0600" → 06:00, "1430" → 14:30
    h = parseInt(digits.slice(0, 2), 10)
    m = parseInt(digits.slice(2, 4), 10)
  }

  if (h < 0 || h > 23 || m < 0 || m > 59) return null
  return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`
}

/** Move a HH:MM time by `deltaMinutes` (positive or negative), wrapping around midnight. */
function adjustTime(time: string, deltaMinutes: number): string {
  const [h, m] = time.split(':').map(Number)
  const totalMins = ((h * 60 + m + deltaMinutes) % (24 * 60) + 24 * 60) % (24 * 60)
  const newH = Math.floor(totalMins / 60)
  const newM = totalMins % 60
  return `${String(newH).padStart(2, '0')}:${String(newM).padStart(2, '0')}`
}

interface TimePickerProps {
  value: string
  onChange: (value: string) => void
  className?: string
  'aria-label'?: string
  disabled?: boolean
}

/**
 * A time input with a 15-minute-increment dropdown, keyboard up/down adjustment,
 * and auto-formatting of compact typed input (e.g. "0600" → "06:00").
 */
function TimePicker({
  value,
  onChange,
  className,
  'aria-label': ariaLabel,
  disabled,
}: TimePickerProps) {
  const [open, setOpen] = useState(false)
  const [inputValue, setInputValue] = useState(value)
  const [isEditing, setIsEditing] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLUListElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // Keep display in sync with external value when not actively editing
  useEffect(() => {
    if (!isEditing) setInputValue(value)
  }, [value, isEditing])

  // Close on click outside
  useEffect(() => {
    if (!open) return
    function handleClick(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [open])

  // Scroll selected option into view when dropdown opens
  useEffect(() => {
    if (!open || !listRef.current) return
    const selected = listRef.current.querySelector('[aria-selected="true"]') as HTMLElement
    selected?.scrollIntoView({ block: 'center' })
  }, [open])

  function commitInput(raw: string) {
    const parsed = parseTimeInput(raw)
    if (parsed) {
      onChange(parsed)
      setInputValue(parsed)
    } else {
      // Revert to last valid value
      setInputValue(value)
    }
    setIsEditing(false)
  }

  function handleInputChange(e: React.ChangeEvent<HTMLInputElement>) {
    setInputValue(e.target.value)
    setIsEditing(true)
    // Auto-commit when user has typed a complete 4-digit time without colon
    const digits = e.target.value.replace(/\D/g, '')
    if (digits.length === 4) {
      const parsed = parseTimeInput(digits)
      if (parsed) {
        onChange(parsed)
        setInputValue(parsed)
        setIsEditing(false)
      }
    }
  }

  function handleInputBlur() {
    commitInput(inputValue)
    // Don't close dropdown here — the user may have clicked an option
  }

  function handleInputKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      const base = parseTimeInput(inputValue) ?? value
      if (!base) return
      const next = adjustTime(base, 15)
      onChange(next)
      setInputValue(next)
      setIsEditing(false)
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      const base = parseTimeInput(inputValue) ?? value
      if (!base) return
      const next = adjustTime(base, -15)
      onChange(next)
      setInputValue(next)
      setIsEditing(false)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      commitInput(inputValue)
      setOpen(false)
    } else if (e.key === 'Escape') {
      setOpen(false)
      setInputValue(value)
      setIsEditing(false)
    } else if (!open && e.key !== 'Tab' && e.key !== 'Shift') {
      setOpen(true)
    }
  }

  function handleSelect(time: string) {
    onChange(time)
    setInputValue(time)
    setIsEditing(false)
    setOpen(false)
    inputRef.current?.focus()
  }

  return (
    <div ref={containerRef} className={cn('relative', className)}>
      <input
        ref={inputRef}
        type="text"
        inputMode="numeric"
        value={inputValue}
        placeholder="HH:MM"
        disabled={disabled}
        aria-label={ariaLabel}
        aria-autocomplete="list"
        aria-expanded={open}
        aria-haspopup="listbox"
        onChange={handleInputChange}
        onBlur={handleInputBlur}
        onFocus={() => setOpen(true)}
        onKeyDown={handleInputKeyDown}
        className={cn(
          'bg-gray-800 text-white rounded px-2 py-1.5 text-sm font-mono w-28',
          'border border-gray-700 focus:border-blue-500 focus:outline-none',
          'disabled:opacity-40 disabled:cursor-not-allowed'
        )}
      />

      {open && (
        <ul
          ref={listRef}
          role="listbox"
          aria-label={ariaLabel}
          className="absolute z-50 top-full mt-1 left-0 bg-gray-800 border border-gray-700 rounded-lg shadow-xl max-h-48 overflow-y-auto min-w-[7rem]"
        >
          {TIME_OPTIONS.map(t => (
            <li
              key={t}
              role="option"
              aria-selected={t === value}
              onMouseDown={e => {
                // Prevent the input's onBlur from firing before we handle the click
                e.preventDefault()
                handleSelect(t)
              }}
              className={cn(
                'px-3 py-1.5 text-sm font-mono cursor-pointer',
                t === value
                  ? 'bg-blue-600/20 text-blue-400'
                  : 'text-gray-200 hover:bg-gray-700'
              )}
            >
              {t}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export { TimePicker }
