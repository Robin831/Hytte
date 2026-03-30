import React, { useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown } from 'lucide-react'
import { cn } from '../../lib/utils'
import { parseTimeInput, adjustTime } from './time-picker-utils'

// All 15-minute increments across a 24-hour day (96 options)
const TIME_OPTIONS: string[] = []
for (let h = 0; h < 24; h++) {
  for (let m = 0; m < 60; m += 15) {
    TIME_OPTIONS.push(`${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`)
  }
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
 *
 * Implements the ARIA combobox + listbox pattern:
 * - The dropdown opens on typing, clicking the input, or clicking the chevron button.
 * - When the dropdown is closed, ArrowUp/Down adjust the value by ±15 min.
 * - When the dropdown is open, ArrowUp/Down navigate the list items.
 */
function TimePicker({
  value,
  onChange,
  className,
  'aria-label': ariaLabel,
  disabled,
}: TimePickerProps) {
  const { t } = useTranslation('common')
  const uid = useId()
  const listboxId = `${uid}-listbox`
  const optionIdPrefix = `${uid}-opt-`

  const [open, setOpen] = useState(false)
  const [inputValue, setInputValue] = useState(value)
  const [isEditing, setIsEditing] = useState(false)
  // Index of the keyboard-highlighted option in the listbox (-1 = none)
  const [activeIndex, setActiveIndex] = useState<number>(-1)

  const containerRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLUListElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // Refs to expose current state to the non-passive wheel event listener
  const isFocused = useRef(false)
  const openRef = useRef(open)
  const inputValueRef = useRef(inputValue)
  const valueRef = useRef(value)
  // Keep these refs in sync during render so native event listeners see fresh state
  openRef.current = open
  inputValueRef.current = inputValue
  valueRef.current = value

  // Keep display in sync with external value when not actively editing
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (!isEditing) setInputValue(value)
  }, [value, isEditing])

  // Close on click outside
  useEffect(() => {
    if (!open) return
    function handleClick(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
        setActiveIndex(-1)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [open])

  // When dropdown opens, initialise activeIndex to the currently selected time
  useEffect(() => {
    if (!open) return
    const idx = TIME_OPTIONS.indexOf(value)
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setActiveIndex(idx)
  }, [open, value])

  // Scroll the highlighted option into view whenever activeIndex changes
  useEffect(() => {
    if (!open || !listRef.current || activeIndex < 0) return
    const item = listRef.current.querySelector<HTMLElement>(`#${CSS.escape(optionIdPrefix + activeIndex)}`)
    item?.scrollIntoView({ block: 'nearest' })
  }, [open, activeIndex, optionIdPrefix])

  // Attach a non-passive wheel listener so we can call preventDefault and block page scroll.
  // Gate on focus + closed dropdown; scroll up = +1 min, scroll down = -1 min, Shift = ±15 min.
  // Clamps to 00:00–23:59 instead of wrapping.
  useEffect(() => {
    const input = inputRef.current
    if (!input) return
    function onWheel(e: WheelEvent) {
      if (!isFocused.current || openRef.current) return
      if (!input || input.disabled) return
      if (e.deltaY === 0) return
      const base = parseTimeInput(inputValueRef.current) ?? valueRef.current
      if (!base) return
      e.preventDefault()
      const step = e.shiftKey ? 15 : 1
      const direction = e.deltaY < 0 ? 1 : -1
      const [h, m] = base.split(':').map(Number)
      const clamped = Math.max(0, Math.min(23 * 60 + 59, h * 60 + m + direction * step))
      const next = `${String(Math.floor(clamped / 60)).padStart(2, '0')}:${String(clamped % 60).padStart(2, '0')}`
      if (next === base) return
      onChange(next)
      inputValueRef.current = next
      setInputValue(next)
      setIsEditing(false)
    }
    input.addEventListener('wheel', onWheel, { passive: false })
    return () => input.removeEventListener('wheel', onWheel)
  }, [onChange])

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
    setActiveIndex(-1)
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

  function handleInputBlur(e: React.FocusEvent<HTMLInputElement>) {
    commitInput(inputValue)
    const nextFocus = e.relatedTarget
    if (!nextFocus || !listRef.current || !listRef.current.contains(nextFocus as Node)) {
      setOpen(false)
      setActiveIndex(-1)
    }
  }

  function handleInputKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (open) {
        // Navigate list downward
        setActiveIndex(prev => Math.min(prev + 1, TIME_OPTIONS.length - 1))
      } else {
        // Adjust value by +15 min when list is closed
        const base = parseTimeInput(inputValue) ?? value
        if (!base) return
        const next = adjustTime(base, 15)
        onChange(next)
        setInputValue(next)
        setIsEditing(false)
      }
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      if (open) {
        // Navigate list upward
        setActiveIndex(prev => Math.max(prev - 1, 0))
      } else {
        // Adjust value by -15 min when list is closed
        const base = parseTimeInput(inputValue) ?? value
        if (!base) return
        const next = adjustTime(base, -15)
        onChange(next)
        setInputValue(next)
        setIsEditing(false)
      }
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (open && activeIndex >= 0) {
        // Select the keyboard-highlighted option
        handleSelect(TIME_OPTIONS[activeIndex])
      } else {
        commitInput(inputValue)
        setOpen(false)
        setActiveIndex(-1)
      }
    } else if (e.key === 'Escape') {
      setOpen(false)
      setActiveIndex(-1)
      setInputValue(value)
      setIsEditing(false)
    } else if (!open && e.key.length === 1 && /[0-9:]/.test(e.key)) {
      setOpen(true)
    }
  }

  function handleSelect(time: string) {
    onChange(time)
    setInputValue(time)
    setIsEditing(false)
    setOpen(false)
    setActiveIndex(-1)
    inputRef.current?.focus()
  }

  const activeOptionId = activeIndex >= 0 ? `${optionIdPrefix}${activeIndex}` : undefined

  function handleInputClick() {
    if (!disabled) setOpen(true)
  }

  function handleChevronClick(e: React.MouseEvent) {
    e.preventDefault()
    if (disabled) return
    setOpen(prev => !prev)
    if (!open) inputRef.current?.focus()
  }

  return (
    <div ref={containerRef} className={cn('relative inline-flex items-center', className)}>
      <input
        ref={inputRef}
        type="text"
        inputMode="numeric"
        role="combobox"
        value={inputValue}
        placeholder={t('timePicker.placeholder')}
        disabled={disabled}
        aria-label={ariaLabel}
        aria-autocomplete="list"
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-controls={listboxId}
        aria-activedescendant={activeOptionId}
        onChange={handleInputChange}
        onFocus={() => { isFocused.current = true }}
        onBlur={(e) => { isFocused.current = false; handleInputBlur(e) }}
        onClick={handleInputClick}
        onKeyDown={handleInputKeyDown}
        className={cn(
          'bg-gray-800 text-white rounded-l px-2 py-1.5 text-sm font-mono w-24',
          'border border-gray-700 focus:border-blue-500 focus:outline-none',
          'disabled:opacity-40 disabled:cursor-not-allowed'
        )}
      />
      <button
        type="button"
        tabIndex={disabled ? -1 : 0}
        disabled={disabled}
        onMouseDown={e => e.preventDefault()}
        onClick={handleChevronClick}
        aria-label={ariaLabel ?? t('timePicker.placeholder')}
        className={cn(
          'bg-gray-800 border border-l-0 border-gray-700 rounded-r px-1.5 py-1.5',
          'text-gray-400 hover:text-white hover:bg-gray-700',
          'disabled:opacity-40 disabled:cursor-not-allowed',
          open && 'text-white bg-gray-700'
        )}
      >
        <ChevronDown size={14} className={cn('transition-transform', open && 'rotate-180')} />
      </button>

      {open && (
        <ul
          ref={listRef}
          id={listboxId}
          role="listbox"
          aria-label={ariaLabel}
          className="absolute z-50 top-full mt-1 left-0 bg-gray-800 border border-gray-700 rounded-lg shadow-xl max-h-48 overflow-y-auto min-w-[7rem]"
        >
          {TIME_OPTIONS.map((time, idx) => (
            <li
              key={time}
              id={`${optionIdPrefix}${idx}`}
              role="option"
              aria-selected={time === value}
              tabIndex={-1}
              onMouseDown={e => {
                // Prevent the input's onBlur from firing before we handle the click
                e.preventDefault()
                handleSelect(time)
              }}
              className={cn(
                'px-3 py-1.5 text-sm font-mono cursor-pointer',
                idx === activeIndex && 'ring-1 ring-inset ring-blue-400',
                time === value
                  ? 'bg-blue-600/20 text-blue-400'
                  : 'text-gray-200 hover:bg-gray-700'
              )}
            >
              {time}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export { TimePicker }
