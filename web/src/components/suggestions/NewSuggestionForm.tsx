import { useEffect, useId, useMemo, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import { Dialog, DialogBody, DialogFooter, DialogHeader } from '../ui/dialog'
import {
  NEW_PAGE_SLUG,
  type Suggestion,
  type SuggestionSize,
  type SuggestionType,
} from './SuggestionCard'

export { NEW_PAGE_SLUG }
export const TITLE_MAX = 120
export const BODY_MAX_BYTES = 4096

const TYPE_OPTIONS: SuggestionType[] = [
  'addition',
  'bugfix',
  'improvement',
  'refactor',
  'new_page',
]

const SIZE_OPTIONS: SuggestionSize[] = ['s', 'm', 'l']

interface PageOption {
  slug: string
  title: string
}

export interface NewSuggestionFormProps {
  open: boolean
  onClose: () => void
  onCreated: (created: Suggestion) => void
}

interface FieldErrors {
  page?: string
  title?: string
  body?: string
  type?: string
  size?: string
}

function byteLength(value: string): number {
  return new TextEncoder().encode(value).length
}

export function NewSuggestionForm({ open, onClose, onCreated }: NewSuggestionFormProps) {
  const { t } = useTranslation('suggestions')
  const { t: tCommon } = useTranslation('common')
  const titleId = useId()
  const pageId = useId()
  const titleFieldId = useId()
  const bodyFieldId = useId()
  const typeId = useId()
  const sizeId = useId()

  const [pages, setPages] = useState<PageOption[]>([])
  const [pagesLoading, setPagesLoading] = useState(false)
  const [pagesError, setPagesError] = useState<string | null>(null)

  const [pageSlug, setPageSlug] = useState('')
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [type, setType] = useState<SuggestionType | ''>('')
  const [size, setSize] = useState<SuggestionSize | ''>('')

  const [errors, setErrors] = useState<FieldErrors>({})
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  // Reset form state and reload pages whenever the dialog opens. setState in
  // an effect is the established pattern in this repo for dialogs that need to
  // re-initialize on each open without unmounting the form (matches
  // TokenCreateDialog).
  useEffect(() => {
    if (!open) return
    /* eslint-disable react-hooks/set-state-in-effect */
    setPageSlug('')
    setTitle('')
    setBody('')
    setType('')
    setSize('')
    setErrors({})
    setSubmitError(null)
    setSubmitting(false)
    setPagesLoading(true)
    setPagesError(null)
    /* eslint-enable react-hooks/set-state-in-effect */

    const controller = new AbortController()
    ;(async () => {
      try {
        const res = await fetch('/api/suggestions/pages', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) {
          throw new Error(t('form.errors.failedToLoadPages'))
        }
        const data = (await res.json()) as PageOption[]
        setPages(Array.isArray(data) ? data : [])
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setPagesError(
          err instanceof Error ? err.message : t('form.errors.failedToLoadPages'),
        )
      } finally {
        if (!controller.signal.aborted) setPagesLoading(false)
      }
    })()
    return () => controller.abort()
  }, [open, t])

  function handleTypeChange(value: string) {
    const next = value as SuggestionType | ''
    setType(next)
    // The backend rejects type=new_page unless page_slug=__new_page__ (and vice
    // versa); keep the slug aligned automatically so the user cannot land in an
    // invalid combination.
    if (next === 'new_page') {
      setPageSlug(NEW_PAGE_SLUG)
    } else if (pageSlug === NEW_PAGE_SLUG) {
      setPageSlug('')
    }
  }

  const visiblePages = useMemo(() => {
    if (type === 'new_page') {
      return pages.filter(p => p.slug === NEW_PAGE_SLUG)
    }
    if (type === '') {
      return pages
    }
    return pages.filter(p => p.slug !== NEW_PAGE_SLUG)
  }, [pages, type])

  const titleLen = title.length
  const bodyBytes = byteLength(body)

  function validate(): FieldErrors {
    const next: FieldErrors = {}
    if (!type) {
      next.type = t('form.errors.typeRequired')
    }
    if (!size) {
      next.size = t('form.errors.sizeRequired')
    }
    if (!pageSlug) {
      next.page = t('form.errors.pageRequired')
    } else if ((type === 'new_page') !== (pageSlug === NEW_PAGE_SLUG)) {
      next.page = t('form.errors.newPageMismatch')
    }
    const trimmedTitle = title.trim()
    if (!trimmedTitle) {
      next.title = t('form.errors.titleRequired')
    } else if (trimmedTitle.length > TITLE_MAX) {
      next.title = t('form.errors.titleTooLong', { max: TITLE_MAX })
    }
    const trimmedBody = body.trim()
    if (!trimmedBody) {
      next.body = t('form.errors.bodyRequired')
    } else if (byteLength(trimmedBody) > BODY_MAX_BYTES) {
      next.body = t('form.errors.bodyTooLong', { max: BODY_MAX_BYTES })
    }
    return next
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (submitting) return
    const next = validate()
    setErrors(next)
    setSubmitError(null)
    if (Object.keys(next).length > 0) return

    setSubmitting(true)
    try {
      const res = await fetch('/api/suggestions', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          type,
          size,
          page_slug: pageSlug,
          title: title.trim(),
          body: body.trim(),
        }),
      })
      if (!res.ok) {
        let msg = t('form.errors.failedToCreate')
        try {
          const data = (await res.json()) as { error?: string }
          if (data?.error) msg = data.error
        } catch {
          // fall back to generic message
        }
        throw new Error(msg)
      }
      const created = (await res.json()) as Suggestion
      onCreated(created)
    } catch (err) {
      setSubmitError(
        err instanceof Error ? err.message : t('form.errors.failedToCreate'),
      )
    } finally {
      setSubmitting(false)
    }
  }

  const titleErrId = `${titleFieldId}-error`
  const bodyErrId = `${bodyFieldId}-error`
  const pageErrId = `${pageId}-error`
  const typeErrId = `${typeId}-error`
  const sizeErrId = `${sizeId}-error`

  return (
    <Dialog
      open={open}
      onClose={submitting ? () => {} : onClose}
      maxWidth="max-w-lg"
      aria-labelledby={titleId}
    >
      <DialogHeader
        id={titleId}
        title={t('form.title')}
        onClose={submitting ? () => {} : onClose}
      />
      <form onSubmit={handleSubmit} noValidate>
        <DialogBody>
          <div className="space-y-4">
            <div>
              <label htmlFor={typeId} className="block text-sm font-medium text-gray-300 mb-1">
                {t('form.fields.type')}
              </label>
              <select
                id={typeId}
                value={type}
                onChange={e => handleTypeChange(e.target.value)}
                disabled={submitting}
                aria-invalid={errors.type ? true : undefined}
                aria-describedby={errors.type ? typeErrId : undefined}
                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 disabled:opacity-50 [color-scheme:dark]"
              >
                <option value="">{t('form.placeholders.type')}</option>
                {TYPE_OPTIONS.map(opt => (
                  <option key={opt} value={opt}>
                    {t(`card.types.${opt}`)}
                  </option>
                ))}
              </select>
              {errors.type && (
                <p id={typeErrId} role="alert" className="mt-1 text-xs text-red-300">
                  {errors.type}
                </p>
              )}
            </div>

            <div>
              <label htmlFor={pageId} className="block text-sm font-medium text-gray-300 mb-1">
                {t('form.fields.page')}
              </label>
              <select
                id={pageId}
                value={pageSlug}
                onChange={e => setPageSlug(e.target.value)}
                disabled={submitting || pagesLoading || type === 'new_page'}
                aria-invalid={errors.page ? true : undefined}
                aria-describedby={errors.page ? pageErrId : undefined}
                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 disabled:opacity-50 [color-scheme:dark]"
              >
                <option value="">
                  {pagesLoading
                    ? tCommon('status.loading')
                    : t('form.placeholders.page')}
                </option>
                {visiblePages.map(p => (
                  <option key={p.slug} value={p.slug}>
                    {p.slug === NEW_PAGE_SLUG ? t('form.newPageOption') : p.title}
                  </option>
                ))}
              </select>
              {pagesError && (
                <p role="alert" className="mt-1 text-xs text-red-300">
                  {pagesError}
                </p>
              )}
              {errors.page && (
                <p id={pageErrId} role="alert" className="mt-1 text-xs text-red-300">
                  {errors.page}
                </p>
              )}
            </div>

            <div>
              <label htmlFor={sizeId} className="block text-sm font-medium text-gray-300 mb-1">
                {t('form.fields.size')}
              </label>
              <select
                id={sizeId}
                value={size}
                onChange={e => setSize(e.target.value as SuggestionSize | '')}
                disabled={submitting}
                aria-invalid={errors.size ? true : undefined}
                aria-describedby={errors.size ? sizeErrId : undefined}
                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 disabled:opacity-50 [color-scheme:dark]"
              >
                <option value="">{t('form.placeholders.size')}</option>
                {SIZE_OPTIONS.map(opt => (
                  <option key={opt} value={opt}>
                    {t(`card.sizes.${opt}`)}
                  </option>
                ))}
              </select>
              {errors.size && (
                <p id={sizeErrId} role="alert" className="mt-1 text-xs text-red-300">
                  {errors.size}
                </p>
              )}
            </div>

            <div>
              <label htmlFor={titleFieldId} className="block text-sm font-medium text-gray-300 mb-1">
                {t('form.fields.title')}
              </label>
              <input
                id={titleFieldId}
                type="text"
                value={title}
                onChange={e => setTitle(e.target.value)}
                placeholder={t('form.placeholders.title')}
                maxLength={TITLE_MAX}
                disabled={submitting}
                aria-invalid={errors.title ? true : undefined}
                aria-describedby={errors.title ? titleErrId : undefined}
                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-blue-500 disabled:opacity-50"
              />
              <div className="mt-1 flex items-start justify-between gap-2">
                {errors.title ? (
                  <p id={titleErrId} role="alert" className="text-xs text-red-300">
                    {errors.title}
                  </p>
                ) : (
                  <span className="text-xs text-gray-500" />
                )}
                <span
                  className={`text-xs shrink-0 ${
                    titleLen > TITLE_MAX ? 'text-red-300' : 'text-gray-500'
                  }`}
                >
                  {t('form.charCount', { count: titleLen, max: TITLE_MAX })}
                </span>
              </div>
            </div>

            <div>
              <label htmlFor={bodyFieldId} className="block text-sm font-medium text-gray-300 mb-1">
                {t('form.fields.body')}
              </label>
              <textarea
                id={bodyFieldId}
                value={body}
                onChange={e => setBody(e.target.value)}
                placeholder={t('form.placeholders.body')}
                rows={6}
                disabled={submitting}
                aria-invalid={errors.body ? true : undefined}
                aria-describedby={errors.body ? bodyErrId : undefined}
                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white placeholder:text-gray-500 focus:outline-none focus:border-blue-500 disabled:opacity-50"
              />
              <div className="mt-1 flex items-start justify-between gap-2">
                {errors.body ? (
                  <p id={bodyErrId} role="alert" className="text-xs text-red-300">
                    {errors.body}
                  </p>
                ) : (
                  <span className="text-xs text-gray-500" />
                )}
                <span
                  className={`text-xs shrink-0 ${
                    bodyBytes > BODY_MAX_BYTES ? 'text-red-300' : 'text-gray-500'
                  }`}
                >
                  {t('form.byteCount', { count: bodyBytes, max: BODY_MAX_BYTES })}
                </span>
              </div>
            </div>

            {submitError && (
              <p
                role="alert"
                data-testid="new-suggestion-submit-error"
                className="text-sm text-red-300"
              >
                {submitError}
              </p>
            )}
          </div>
        </DialogBody>
        <DialogFooter>
          <button
            type="button"
            onClick={onClose}
            disabled={submitting}
            className="px-4 py-2 text-sm text-gray-300 hover:text-white transition-colors disabled:opacity-50"
          >
            {t('form.actions.cancel')}
          </button>
          <button
            type="submit"
            disabled={submitting}
            className="inline-flex items-center gap-2 rounded-lg border border-blue-500/40 bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {submitting && <Loader2 size={14} className="animate-spin" aria-hidden={true} />}
            <span>
              {submitting
                ? t('form.actions.submitting')
                : t('form.actions.submit')}
            </span>
          </button>
        </DialogFooter>
      </form>
    </Dialog>
  )
}
