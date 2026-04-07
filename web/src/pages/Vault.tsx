import { useState, useEffect, useRef, useMemo } from 'react'
import {
  Upload,
  Search,
  FolderOpen,
  Tag,
  Trash2,
  Download,
  Eye,
  Edit3,
  X,
  File,
  Image,
  FileText,
  Plus,
  Save,
  ChevronDown,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ConfirmDialog } from '../components/ui/dialog'

interface VaultFile {
  id: number
  user_id: number
  filename: string
  mime_type: string
  size_bytes: number
  folder: string
  access: string
  tags: string[]
  created_at: string
  updated_at: string
}

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function fileIcon(mimeType: string) {
  if (mimeType.startsWith('image/')) return <Image size={20} className="text-blue-400" />
  if (mimeType === 'application/pdf') return <FileText size={20} className="text-red-400" />
  if (mimeType.startsWith('text/')) return <FileText size={20} className="text-green-400" />
  return <File size={20} className="text-gray-400" />
}

function isPreviewable(mimeType: string): boolean {
  return mimeType.startsWith('image/') || mimeType === 'application/pdf' || mimeType.startsWith('text/')
}

export default function Vault() {
  const { t, i18n } = useTranslation('vault')
  const [files, setFiles] = useState<VaultFile[]>([])
  const [folders, setFolders] = useState<string[]>([])
  const [allTags, setAllTags] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [search, setSearch] = useState('')
  const [activeFolder, setActiveFolder] = useState('')
  const [activeTag, setActiveTag] = useState('')
  const [uploading, setUploading] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)
  const [deleteTarget, setDeleteTarget] = useState<VaultFile | null>(null)
  const [editFile, setEditFile] = useState<VaultFile | null>(null)
  const [previewFile, setPreviewFile] = useState<VaultFile | null>(null)
  const [previewUrl, setPreviewUrl] = useState<string | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const uploadAbortRef = useRef<AbortController | null>(null)

  // Edit form state
  const [editFilename, setEditFilename] = useState('')
  const [editFolder, setEditFolder] = useState('')
  const [editAccess, setEditAccess] = useState('private')
  const [editTags, setEditTags] = useState('')
  const [saving, setSaving] = useState(false)

  // Upload form state
  const [showUploadForm, setShowUploadForm] = useState(false)
  const [uploadFolder, setUploadFolder] = useState('')
  const [uploadAccess, setUploadAccess] = useState('private')
  const [uploadTags, setUploadTags] = useState('')

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      setLoading(true)
      try {
        const params = new URLSearchParams()
        if (search) params.set('search', search)
        if (activeFolder) params.set('folder', activeFolder)
        if (activeTag) params.set('tag', activeTag)
        const res = await fetch(`/api/vault/files?${params}`, {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(t('errors.failedToLoad'))
        const data = await res.json()
        setFiles(data.files ?? [])
        setError('')
      } catch (err) {
        if (err instanceof Error && err.name !== 'AbortError') {
          setError(err.message)
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    })()
    return () => controller.abort()
  }, [search, activeFolder, activeTag, refreshKey, t])

  useEffect(() => {
    const controller = new AbortController()
    ;(async () => {
      try {
        const [foldersRes, tagsRes] = await Promise.all([
          fetch('/api/vault/folders', { credentials: 'include', signal: controller.signal }),
          fetch('/api/vault/tags', { credentials: 'include', signal: controller.signal }),
        ])
        if (foldersRes.ok) {
          const data = await foldersRes.json()
          setFolders(data.folders ?? [])
        }
        if (tagsRes.ok) {
          const data = await tagsRes.json()
          setAllTags(data.tags ?? [])
        }
      } catch {
        // non-critical
      }
    })()
    return () => controller.abort()
  }, [refreshKey])

  const handleUpload = async (fileList: FileList | null) => {
    if (!fileList || fileList.length === 0) return
    uploadAbortRef.current?.abort()
    const controller = new AbortController()
    uploadAbortRef.current = controller
    setUploading(true)
    setError('')

    try {
      for (const file of Array.from(fileList)) {
        const formData = new FormData()
        formData.append('file', file)
        if (uploadFolder) formData.append('folder', uploadFolder)
        formData.append('access', uploadAccess)
        if (uploadTags) formData.append('tags', uploadTags)

        const res = await fetch('/api/vault/files', {
          method: 'POST',
          credentials: 'include',
          body: formData,
          signal: controller.signal,
        })
        if (!res.ok) {
          const data = await res.json().catch(() => ({ error: t('errors.failedToUpload') }))
          throw new Error(data.error || t('errors.failedToUpload'))
        }
      }
      setShowUploadForm(false)
      setUploadFolder('')
      setUploadAccess('private')
      setUploadTags('')
      setRefreshKey((k) => k + 1)
    } catch (err) {
      if (err instanceof Error && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : t('errors.failedToUpload'))
    } finally {
      setUploading(false)
      if (fileInputRef.current) fileInputRef.current.value = ''
    }
  }

  const handleDelete = async (file: VaultFile) => {
    try {
      const res = await fetch(`/api/vault/files/${file.id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) throw new Error(t('errors.failedToDelete'))
      setRefreshKey((k) => k + 1)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.failedToDelete'))
    }
  }

  const handleEdit = (file: VaultFile) => {
    setEditFile(file)
    setEditFilename(file.filename)
    setEditFolder(file.folder)
    setEditAccess(file.access)
    setEditTags(file.tags.join(', '))
  }

  const handleSaveEdit = async () => {
    if (!editFile) return
    setSaving(true)
    try {
      const res = await fetch(`/api/vault/files/${editFile.id}`, {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          filename: editFilename,
          folder: editFolder,
          access: editAccess,
          tags: editTags
            .split(',')
            .map((t) => t.trim())
            .filter(Boolean),
        }),
      })
      if (!res.ok) throw new Error(t('errors.failedToUpdate'))
      setEditFile(null)
      setRefreshKey((k) => k + 1)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.failedToUpdate'))
    } finally {
      setSaving(false)
    }
  }

  const openPreview = async (file: VaultFile) => {
    setPreviewFile(file)
    try {
      const res = await fetch(`/api/vault/files/${file.id}/preview`, { credentials: 'include' })
      if (!res.ok) throw new Error(t('errors.failedToPreview'))
      const blob = await res.blob()
      setPreviewUrl(URL.createObjectURL(blob))
    } catch (err) {
      setError(err instanceof Error ? err.message : t('errors.failedToPreview'))
      setPreviewFile(null)
    }
  }

  const closePreview = () => {
    if (previewUrl) URL.revokeObjectURL(previewUrl)
    setPreviewUrl(null)
    setPreviewFile(null)
  }

  const handleDownload = (file: VaultFile) => {
    const a = document.createElement('a')
    a.href = `/api/vault/files/${file.id}/download`
    a.download = file.filename
    a.click()
  }

  const dateFormatter = useMemo(
    () => new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }),
    [i18n.language],
  )

  return (
    <div className="max-w-6xl mx-auto px-4 py-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 mb-6">
        <h1 className="text-2xl font-bold">{t('title')}</h1>
        <button
          onClick={() => setShowUploadForm(true)}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded-lg text-sm font-medium transition-colors cursor-pointer"
        >
          <Plus size={16} />
          {t('upload')}
        </button>
      </div>

      {/* Error */}
      {error && (
        <div className="bg-red-900/50 border border-red-700 text-red-200 px-4 py-3 rounded-lg mb-4">
          {error}
        </div>
      )}

      {/* Search and filters */}
      <div className="flex flex-col sm:flex-row gap-3 mb-6">
        <div className="relative flex-1">
          <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t('searchPlaceholder')}
            aria-label={t('searchLabel')}
            className="w-full pl-9 pr-3 py-2 bg-gray-800 border border-gray-700 rounded-lg text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
          />
        </div>

        {folders.length > 0 && (
          <div className="relative">
            <FolderOpen size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500" />
            <select
              value={activeFolder}
              onChange={(e) => setActiveFolder(e.target.value)}
              aria-label={t('filterFolder')}
              className="pl-9 pr-8 py-2 bg-gray-800 border border-gray-700 rounded-lg text-sm text-white appearance-none cursor-pointer focus:outline-none focus:border-blue-500"
            >
              <option value="">{t('allFolders')}</option>
              {folders.map((f) => (
                <option key={f} value={f}>
                  {f}
                </option>
              ))}
            </select>
            <ChevronDown
              size={14}
              className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 pointer-events-none"
            />
          </div>
        )}

        {allTags.length > 0 && (
          <div className="flex gap-1 flex-wrap items-center">
            <Tag size={14} className="text-gray-500 shrink-0" />
            <button
              onClick={() => setActiveTag('')}
              className={`px-2 py-1 rounded text-xs transition-colors cursor-pointer ${
                activeTag === '' ? 'bg-blue-600 text-white' : 'bg-gray-800 text-gray-400 hover:text-white'
              }`}
            >
              {t('tagAll')}
            </button>
            {allTags.map((tag) => (
              <button
                key={tag}
                onClick={() => setActiveTag(tag === activeTag ? '' : tag)}
                className={`px-2 py-1 rounded text-xs transition-colors cursor-pointer ${
                  tag === activeTag ? 'bg-blue-600 text-white' : 'bg-gray-800 text-gray-400 hover:text-white'
                }`}
              >
                {tag}
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Upload form modal */}
      {showUploadForm && (
        <div className="fixed inset-0 bg-black/60 z-50 flex items-center justify-center p-4" onClick={() => setShowUploadForm(false)}>
          <div
            className="bg-gray-900 border border-gray-700 rounded-xl p-6 w-full max-w-md"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-lg font-semibold">{t('uploadTitle')}</h2>
              <button onClick={() => setShowUploadForm(false)} className="text-gray-400 hover:text-white cursor-pointer">
                <X size={20} />
              </button>
            </div>
            <div className="space-y-4">
              <div>
                <label className="block text-sm text-gray-400 mb-1">{t('fields.folder')}</label>
                <input
                  type="text"
                  value={uploadFolder}
                  onChange={(e) => setUploadFolder(e.target.value)}
                  placeholder={t('fields.folderPlaceholder')}
                  className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded-lg text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
                />
              </div>
              <div>
                <label className="block text-sm text-gray-400 mb-1">{t('fields.access')}</label>
                <select
                  value={uploadAccess}
                  onChange={(e) => setUploadAccess(e.target.value)}
                  className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded-lg text-sm text-white cursor-pointer focus:outline-none focus:border-blue-500"
                >
                  <option value="private">{t('accessPrivate')}</option>
                  <option value="shared">{t('accessShared')}</option>
                </select>
              </div>
              <div>
                <label className="block text-sm text-gray-400 mb-1">{t('fields.tags')}</label>
                <input
                  type="text"
                  value={uploadTags}
                  onChange={(e) => setUploadTags(e.target.value)}
                  placeholder={t('fields.tagsPlaceholder')}
                  className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded-lg text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
                />
              </div>
              <div>
                <input
                  ref={fileInputRef}
                  type="file"
                  multiple
                  onChange={(e) => handleUpload(e.target.files)}
                  className="hidden"
                  id="vault-file-input"
                  aria-label={t('chooseFiles')}
                />
                <button
                  onClick={() => fileInputRef.current?.click()}
                  disabled={uploading}
                  className="w-full flex items-center justify-center gap-2 px-4 py-3 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 rounded-lg text-sm font-medium transition-colors cursor-pointer border-2 border-dashed border-blue-500"
                >
                  <Upload size={16} />
                  {uploading ? t('uploading') : t('chooseFiles')}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* File list */}
      {loading ? (
        <div className="text-gray-400 text-sm">{t('loading')}</div>
      ) : files.length === 0 ? (
        <div className="text-center py-16">
          <File size={48} className="mx-auto mb-4 text-gray-600" />
          <p className="text-gray-400 mb-2">{t('empty.message')}</p>
          <p className="text-gray-500 text-sm">{t('empty.uploadFirst')}</p>
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-gray-800 text-gray-400 text-left">
                <th className="pb-2 pr-4 font-medium">{t('columns.name')}</th>
                <th className="pb-2 pr-4 font-medium hidden sm:table-cell">{t('columns.size')}</th>
                <th className="pb-2 pr-4 font-medium hidden sm:table-cell">{t('columns.folder')}</th>
                <th className="pb-2 pr-4 font-medium hidden md:table-cell">{t('columns.updated')}</th>
                <th className="pb-2 font-medium text-right">{t('columns.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {files.map((file) => (
                <tr key={file.id} className="border-b border-gray-800/50 hover:bg-gray-800/30">
                  <td className="py-3 pr-4">
                    <div className="flex items-center gap-2">
                      {fileIcon(file.mime_type)}
                      <div className="min-w-0">
                        <div className="truncate font-medium">{file.filename}</div>
                        {file.tags.length > 0 && (
                          <div className="flex gap-1 mt-0.5 flex-wrap">
                            {file.tags.map((tag) => (
                              <span
                                key={tag}
                                className="px-1.5 py-0.5 bg-gray-800 rounded text-[10px] text-gray-400"
                              >
                                {tag}
                              </span>
                            ))}
                          </div>
                        )}
                      </div>
                    </div>
                  </td>
                  <td className="py-3 pr-4 text-gray-400 hidden sm:table-cell">
                    {formatFileSize(file.size_bytes)}
                  </td>
                  <td className="py-3 pr-4 text-gray-400 hidden sm:table-cell">
                    {file.folder || '-'}
                  </td>
                  <td className="py-3 pr-4 text-gray-400 hidden md:table-cell whitespace-nowrap">
                    {dateFormatter.format(new Date(file.updated_at))}
                  </td>
                  <td className="py-3">
                    <div className="flex items-center justify-end gap-1">
                      {isPreviewable(file.mime_type) && (
                        <button
                          onClick={() => openPreview(file)}
                          className="p-1.5 text-gray-400 hover:text-white transition-colors cursor-pointer"
                          title={t('preview')}
                          aria-label={`${t('preview')} ${file.filename}`}
                        >
                          <Eye size={16} />
                        </button>
                      )}
                      <button
                        onClick={() => handleDownload(file)}
                        className="p-1.5 text-gray-400 hover:text-white transition-colors cursor-pointer"
                        title={t('download')}
                        aria-label={`${t('download')} ${file.filename}`}
                      >
                        <Download size={16} />
                      </button>
                      <button
                        onClick={() => handleEdit(file)}
                        className="p-1.5 text-gray-400 hover:text-white transition-colors cursor-pointer"
                        title={t('edit')}
                        aria-label={`${t('edit')} ${file.filename}`}
                      >
                        <Edit3 size={16} />
                      </button>
                      <button
                        onClick={() => setDeleteTarget(file)}
                        className="p-1.5 text-gray-400 hover:text-red-400 transition-colors cursor-pointer"
                        title={t('delete')}
                        aria-label={`${t('delete')} ${file.filename}`}
                      >
                        <Trash2 size={16} />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Edit modal */}
      {editFile && (
        <div className="fixed inset-0 bg-black/60 z-50 flex items-center justify-center p-4" onClick={() => setEditFile(null)}>
          <div
            className="bg-gray-900 border border-gray-700 rounded-xl p-6 w-full max-w-md"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-lg font-semibold">{t('editTitle')}</h2>
              <button onClick={() => setEditFile(null)} className="text-gray-400 hover:text-white cursor-pointer">
                <X size={20} />
              </button>
            </div>
            <div className="space-y-4">
              <div>
                <label className="block text-sm text-gray-400 mb-1">{t('fields.filename')}</label>
                <input
                  type="text"
                  value={editFilename}
                  onChange={(e) => setEditFilename(e.target.value)}
                  className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded-lg text-sm text-white focus:outline-none focus:border-blue-500"
                />
              </div>
              <div>
                <label className="block text-sm text-gray-400 mb-1">{t('fields.folder')}</label>
                <input
                  type="text"
                  value={editFolder}
                  onChange={(e) => setEditFolder(e.target.value)}
                  placeholder={t('fields.folderPlaceholder')}
                  className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded-lg text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
                />
              </div>
              <div>
                <label className="block text-sm text-gray-400 mb-1">{t('fields.access')}</label>
                <select
                  value={editAccess}
                  onChange={(e) => setEditAccess(e.target.value)}
                  className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded-lg text-sm text-white cursor-pointer focus:outline-none focus:border-blue-500"
                >
                  <option value="private">{t('accessPrivate')}</option>
                  <option value="shared">{t('accessShared')}</option>
                </select>
              </div>
              <div>
                <label className="block text-sm text-gray-400 mb-1">{t('fields.tags')}</label>
                <input
                  type="text"
                  value={editTags}
                  onChange={(e) => setEditTags(e.target.value)}
                  placeholder={t('fields.tagsPlaceholder')}
                  className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded-lg text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500"
                />
              </div>
              <button
                onClick={handleSaveEdit}
                disabled={saving || !editFilename.trim()}
                className="w-full flex items-center justify-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 rounded-lg text-sm font-medium transition-colors cursor-pointer"
              >
                <Save size={16} />
                {saving ? t('saving') : t('save')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Preview modal */}
      {previewFile && (
        <div className="fixed inset-0 bg-black/80 z-50 flex items-center justify-center p-4" onClick={closePreview}>
          <div
            className="bg-gray-900 border border-gray-700 rounded-xl w-full max-w-4xl max-h-[90vh] flex flex-col"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between p-4 border-b border-gray-700">
              <h2 className="text-sm font-medium truncate">{previewFile.filename}</h2>
              <button onClick={closePreview} className="text-gray-400 hover:text-white cursor-pointer shrink-0 ml-2">
                <X size={20} />
              </button>
            </div>
            <div className="flex-1 overflow-auto p-4 flex items-center justify-center min-h-0">
              {!previewUrl ? (
                <div className="text-gray-400 text-sm">{t('loading')}</div>
              ) : previewFile.mime_type.startsWith('image/') ? (
                <img src={previewUrl} alt={previewFile.filename} className="max-w-full max-h-full object-contain" />
              ) : previewFile.mime_type === 'application/pdf' ? (
                <iframe src={previewUrl} className="w-full h-full min-h-[60vh]" title={previewFile.filename} />
              ) : (
                <iframe src={previewUrl} className="w-full h-full min-h-[60vh] bg-white" title={previewFile.filename} />
              )}
            </div>
          </div>
        </div>
      )}

      {/* Delete confirmation */}
      <ConfirmDialog
        open={!!deleteTarget}
        title={t('confirmDelete', { title: deleteTarget?.filename ?? '' })}
        onConfirm={() => {
          if (deleteTarget) handleDelete(deleteTarget)
          setDeleteTarget(null)
        }}
        onClose={() => setDeleteTarget(null)}
        variant="destructive"
      />
    </div>
  )
}
