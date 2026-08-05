interface DecimalFieldProps {
  id: string
  value: string
  onChange: (value: string) => void
  /** Renders a `<label htmlFor>`; omit and pass `ariaLabel` for label-less rows. */
  label?: string
  ariaLabel?: string
  placeholder?: string
  /** Inline message rendered under the field, and marks the field invalid. */
  error?: string | null
  /** Marks the field invalid without rendering a message (shared error slots). */
  invalid?: boolean
  /** Tailwind classes for the input; defaults to the salary form field style. */
  inputClassName?: string
}

const DEFAULT_INPUT_CLASS =
  'w-full bg-gray-700 text-white rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-1'

/**
 * Text input for decimal amounts. Deliberately `type="text"` with
 * inputMode="decimal": Chrome and Edge report an empty value for a number input
 * containing a comma, so "7,5" would never reach the parser. The raw text is
 * kept verbatim in state (no parsing on change) so partially typed values like
 * "0," survive a re-render with the caret intact.
 */
export default function DecimalField({
  id,
  value,
  onChange,
  label,
  ariaLabel,
  placeholder,
  error,
  invalid,
  inputClassName = DEFAULT_INPUT_CLASS,
}: DecimalFieldProps) {
  const showInvalid = Boolean(error) || Boolean(invalid)

  return (
    <div>
      {label && (
        <label htmlFor={id} className="block text-xs text-gray-400 mb-1">{label}</label>
      )}
      <input
        id={id}
        type="text"
        inputMode="decimal"
        value={value}
        onChange={e => onChange(e.target.value)}
        placeholder={placeholder}
        aria-label={label ? undefined : ariaLabel}
        aria-invalid={showInvalid || undefined}
        aria-describedby={error ? `${id}-error` : undefined}
        className={`${inputClassName} ${showInvalid ? 'ring-1 ring-red-500 focus:ring-red-500' : 'focus:ring-blue-500'}`}
      />
      {error && (
        <p id={`${id}-error`} className="mt-1 text-xs text-red-400">{error}</p>
      )}
    </div>
  )
}
