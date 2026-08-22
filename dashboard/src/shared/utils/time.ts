export function formatRelative(dateStr: string | undefined): string {
  if (!dateStr) return '—'
  const date = new Date(dateStr)
  const now = Date.now()
  const diff = Math.abs(now - date.getTime())
  const s = Math.floor(diff / 1000)
  if (s < 60) return `${s}s ago`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  const d = Math.floor(h / 24)
  return `${d}d ago`
}

export function formatDuration(ms: number | undefined): string {
  if (!ms) return '—'
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  const rem = s % 60
  if (m < 60) return `${m}m ${rem}s`
  const h = Math.floor(m / 60)
  const remM = m % 60
  return `${h}h ${remM}m`
}

export function formatDateTime(dateStr: string | undefined): string {
  if (!dateStr) return '—'
  return new Date(dateStr).toLocaleString()
}

export function formatDate(dateStr: string | undefined): string {
  if (!dateStr) return '—'
  return new Date(dateStr).toLocaleDateString()
}
