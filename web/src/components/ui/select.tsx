import { useCallback, useEffect, useId, useRef, useState } from 'react'
import { Check, ChevronDown } from 'lucide-react'
import { cn } from '../../lib/utils'

export interface SelectOption {
  value: string
  label: string
  icon?: string
  disabled?: boolean
}

interface SelectProps {
  value: string
  onChange: (value: string) => void
  options: SelectOption[]
  placeholder?: string
  disabled?: boolean
  className?: string
  'aria-label'?: string
  id?: string
}

function Select({
  value,
  onChange,
  options,
  placeholder,
  disabled,
  className,
  'aria-label': ariaLabel,
  id,
}: SelectProps) {
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLUListElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const instanceId = useId()

  const selectedOption = options.find(o => o.value === value)

  const close = useCallback(() => setOpen(false), [])

  // Close on click outside
  useEffect(() => {
    if (!open) return
    function handleClick(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        close()
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [open, close])

  // Arrow key navigation, Escape, and open-on-arrow for the trigger
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (!containerRef.current || !containerRef.current.contains(e.target as Node)) {
        return
      }
      // When closed: ArrowDown/Up on the trigger opens the list
      if (!open) {
        if ((e.key === 'ArrowDown' || e.key === 'ArrowUp') && e.target === buttonRef.current) {
          e.preventDefault()
          setOpen(true)
        }
        return
      }
      if (e.key === 'Escape') {
        close()
        buttonRef.current?.focus()
        return
      }
      if (e.key === 'Tab') {
        close()
        return
      }
      if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
      e.preventDefault()

      const items = listRef.current?.querySelectorAll<HTMLLIElement>(
        '[role="option"]:not([aria-disabled="true"])'
      )
      if (!items || items.length === 0) return

      const activeIndex = Array.from(items).findIndex(el => el === document.activeElement)
      let nextIndex: number
      if (e.key === 'ArrowDown') {
        nextIndex = activeIndex === -1 ? 0 : (activeIndex + 1) % items.length
      } else {
        nextIndex = activeIndex === -1 ? items.length - 1 : (activeIndex - 1 + items.length) % items.length
      }
      items[nextIndex].focus()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [open, close])

  // Scroll selected item into view on open
  useEffect(() => {
    if (!open || !listRef.current) return
    const selected = listRef.current.querySelector('[aria-selected="true"]') as HTMLElement
    selected?.scrollIntoView({ block: 'nearest' })
  }, [open])

  function handleSelect(val: string) {
    onChange(val)
    close()
    buttonRef.current?.focus()
  }

  function renderLabel(opt: SelectOption) {
    return opt.icon ? `${opt.icon} ${opt.label}` : opt.label
  }

  return (
    <div ref={containerRef} className={cn('relative', className)}>
      <button
        ref={buttonRef}
        type="button"
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-controls={open ? `select-list-${instanceId}` : undefined}
        aria-label={ariaLabel}
        id={id}
        disabled={disabled}
        onClick={() => setOpen(v => !v)}
        className={cn(
          'flex items-center justify-between gap-2 w-full',
          'bg-gray-800 text-white rounded px-2 py-1.5 text-sm',
          'border border-gray-700 focus:border-blue-500 focus:outline-none',
          'hover:border-gray-600 transition-colors cursor-pointer',
          'disabled:opacity-40 disabled:cursor-not-allowed',
          !selectedOption && 'text-gray-400'
        )}
      >
        <span className="truncate">
          {selectedOption ? renderLabel(selectedOption) : (placeholder ?? '')}
        </span>
        <ChevronDown
          size={14}
          className={cn('shrink-0 text-gray-400 transition-transform duration-150', open && 'rotate-180')}
        />
      </button>

      {open && (
        <ul
          ref={listRef}
          id={`select-list-${instanceId}`}
          role="listbox"
          aria-label={ariaLabel}
          className="absolute z-50 top-full mt-1 left-0 right-0 bg-gray-800 border border-gray-700 rounded-lg shadow-xl max-h-60 overflow-y-auto"
        >
          {placeholder && (
            <li
              role="option"
              aria-selected={value === ''}
              tabIndex={0}
              onClick={() => handleSelect('')}
              onKeyDown={e => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  e.stopPropagation()
                  handleSelect('')
                }
              }}
              className={cn(
                'flex items-center gap-2 px-3 py-2 text-sm cursor-pointer',
                'text-gray-400 hover:bg-gray-700 focus:bg-gray-700 focus:outline-none'
              )}
            >
              {placeholder}
            </li>
          )}
          {options.map(opt => (
            <li
              key={opt.value}
              role="option"
              aria-selected={opt.value === value}
              aria-disabled={opt.disabled ?? false}
              tabIndex={opt.disabled ? -1 : 0}
              onClick={() => !opt.disabled && handleSelect(opt.value)}
              onKeyDown={e => {
                if ((e.key === 'Enter' || e.key === ' ') && !opt.disabled) {
                  e.preventDefault()
                  e.stopPropagation()
                  handleSelect(opt.value)
                }
              }}
              className={cn(
                'flex items-center gap-2 px-3 py-2 text-sm cursor-pointer focus:outline-none',
                opt.value === value
                  ? 'bg-blue-600/20 text-blue-400 hover:bg-blue-600/30 focus:bg-blue-600/30'
                  : 'text-gray-200 hover:bg-gray-700 focus:bg-gray-700',
                opt.disabled && 'opacity-40 cursor-not-allowed'
              )}
            >
              {opt.icon && <span>{opt.icon}</span>}
              <span className="flex-1 truncate">{opt.label}</span>
              {opt.value === value && <Check size={14} className="shrink-0" />}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export { Select }
