import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { ArrowLeft, Save, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

interface HomeworkProfile {
  id: number
  kid_id: number
  age: number
  grade_level: string
  subjects: string[]
  preferred_language: string
  school_name: string
  current_topics: string[]
  created_at: string
  updated_at: string
}

const AVAILABLE_SUBJECTS = [
  'math',
  'reading',
  'writing',
  'science',
  'social_studies',
  'english',
  'norwegian',
  'thai',
  'art',
  'music',
  'physical_education',
] as const

const GRADE_LEVELS = [
  '1',
  '2',
  '3',
  '4',
  '5',
  '6',
  '7',
  '8',
  '9',
  '10',
] as const

const LANGUAGES = ['en', 'nb', 'th'] as const

export default function HomeworkSettings() {
  const { t } = useTranslation('homework')
  const navigate = useNavigate()

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')

  const [age, setAge] = useState(0)
  const [gradeLevel, setGradeLevel] = useState('')
  const [subjects, setSubjects] = useState<string[]>([])
  const [preferredLanguage, setPreferredLanguage] = useState('')
  const [schoolName, setSchoolName] = useState('')
  const [currentTopics, setCurrentTopics] = useState('')

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        const res = await fetch('/api/homework/profile', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(t('settings.errors.failedToLoad'))
        const data = await res.json()
        const profile = data.profile as HomeworkProfile | null
        if (profile) {
          setAge(profile.age)
          setGradeLevel(profile.grade_level)
          setSubjects(profile.subjects ?? [])
          setPreferredLanguage(profile.preferred_language)
          setSchoolName(profile.school_name)
          setCurrentTopics((profile.current_topics ?? []).join('\n'))
        }
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => { controller.abort() }
  }, [t])

  function toggleSubject(subject: string) {
    setSubjects(prev =>
      prev.includes(subject)
        ? prev.filter(s => s !== subject)
        : [...prev, subject]
    )
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setSaving(true)
    setError('')
    setSuccess('')

    const topicsArray = currentTopics
      .split('\n')
      .map(s => s.trim())
      .filter(Boolean)

    try {
      const res = await fetch('/api/homework/profile', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          age,
          grade_level: gradeLevel,
          subjects,
          preferred_language: preferredLanguage,
          school_name: schoolName,
          current_topics: topicsArray,
        }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => null)
        throw new Error(data?.error || t('settings.errors.failedToSave'))
      }
      setSuccess(t('settings.saved'))
    } catch (err) {
      if (err instanceof Error) setError(err.message)
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <div className="flex justify-center py-12">
        <Loader2 size={32} className="animate-spin text-gray-400" />
      </div>
    )
  }

  return (
    <div className="max-w-2xl mx-auto px-4 py-6">
      <div className="flex items-center gap-3 mb-6">
        <button
          onClick={() => navigate('/homework')}
          className="p-1.5 rounded-lg hover:bg-gray-800 transition-colors cursor-pointer"
          aria-label={t('backToList')}
        >
          <ArrowLeft size={20} />
        </button>
        <h1 className="text-xl font-semibold">{t('settings.title')}</h1>
      </div>

      {error && (
        <div className="mb-4 px-4 py-2 bg-red-900/50 border border-red-800 rounded-lg text-red-300 text-sm">
          {error}
        </div>
      )}

      {success && (
        <div className="mb-4 px-4 py-2 bg-green-900/50 border border-green-800 rounded-lg text-green-300 text-sm">
          {success}
        </div>
      )}

      <form onSubmit={handleSubmit} className="space-y-6">
        {/* Age */}
        <div>
          <label htmlFor="hw-age" className="block text-sm font-medium text-gray-300 mb-1">
            {t('settings.age')}
          </label>
          <input
            id="hw-age"
            type="number"
            min={0}
            max={25}
            value={age || ''}
            onChange={e => setAge(parseInt(e.target.value, 10) || 0)}
            className="w-full sm:w-32 px-3 py-2 bg-gray-800 border border-gray-700 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>

        {/* Grade level */}
        <div>
          <label htmlFor="hw-grade" className="block text-sm font-medium text-gray-300 mb-1">
            {t('settings.gradeLevel')}
          </label>
          <select
            id="hw-grade"
            value={gradeLevel}
            onChange={e => setGradeLevel(e.target.value)}
            className="w-full sm:w-48 px-3 py-2 bg-gray-800 border border-gray-700 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            <option value="">{t('settings.selectGrade')}</option>
            {GRADE_LEVELS.map(g => (
              <option key={g} value={g}>
                {t('settings.grade', { n: g })}
              </option>
            ))}
          </select>
        </div>

        {/* Preferred language */}
        <div>
          <label htmlFor="hw-lang" className="block text-sm font-medium text-gray-300 mb-1">
            {t('settings.preferredLanguage')}
          </label>
          <select
            id="hw-lang"
            value={preferredLanguage}
            onChange={e => setPreferredLanguage(e.target.value)}
            className="w-full sm:w-48 px-3 py-2 bg-gray-800 border border-gray-700 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
          >
            <option value="">{t('settings.selectLanguage')}</option>
            {LANGUAGES.map(lang => (
              <option key={lang} value={lang}>
                {t(`settings.languages.${lang}`)}
              </option>
            ))}
          </select>
        </div>

        {/* Subjects (multi-select as checkboxes) */}
        <fieldset>
          <legend className="block text-sm font-medium text-gray-300 mb-2">
            {t('settings.subjects')}
          </legend>
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
            {AVAILABLE_SUBJECTS.map(subject => (
              <label
                key={subject}
                className={`flex items-center gap-2 px-3 py-2 rounded-lg border cursor-pointer transition-colors ${
                  subjects.includes(subject)
                    ? 'bg-blue-900/50 border-blue-700 text-blue-200'
                    : 'bg-gray-800 border-gray-700 text-gray-300 hover:border-gray-600'
                }`}
              >
                <input
                  type="checkbox"
                  checked={subjects.includes(subject)}
                  onChange={() => toggleSubject(subject)}
                  className="sr-only"
                />
                <span className={`w-4 h-4 rounded border flex items-center justify-center shrink-0 ${
                  subjects.includes(subject)
                    ? 'bg-blue-600 border-blue-500'
                    : 'border-gray-600'
                }`}>
                  {subjects.includes(subject) && (
                    <svg className="w-3 h-3 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={3}>
                      <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                    </svg>
                  )}
                </span>
                <span className="text-sm">{t(`settings.subjectNames.${subject}`)}</span>
              </label>
            ))}
          </div>
        </fieldset>

        {/* School name */}
        <div>
          <label htmlFor="hw-school" className="block text-sm font-medium text-gray-300 mb-1">
            {t('settings.schoolName')}
          </label>
          <input
            id="hw-school"
            type="text"
            value={schoolName}
            onChange={e => setSchoolName(e.target.value)}
            placeholder={t('settings.schoolNamePlaceholder')}
            className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>

        {/* Current topics */}
        <div>
          <label htmlFor="hw-topics" className="block text-sm font-medium text-gray-300 mb-1">
            {t('settings.currentTopics')}
          </label>
          <p className="text-xs text-gray-500 mb-1">{t('settings.currentTopicsHint')}</p>
          <textarea
            id="hw-topics"
            value={currentTopics}
            onChange={e => setCurrentTopics(e.target.value)}
            rows={4}
            placeholder={t('settings.currentTopicsPlaceholder')}
            className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded-lg text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y"
          />
        </div>

        {/* Save button */}
        <div className="pt-2">
          <button
            type="submit"
            disabled={saving}
            className="flex items-center gap-2 px-6 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {saving ? (
              <Loader2 size={16} className="animate-spin" />
            ) : (
              <Save size={16} />
            )}
            {t('settings.save')}
          </button>
        </div>
      </form>
    </div>
  )
}
