interface BrowserLocation {
  origin: string
  protocol: string
}

export const resolveCompanionUrl = (
  configured: string | undefined,
  defaultPort: number,
  location: BrowserLocation | undefined = typeof window === 'undefined' ? undefined : window.location,
): string => {
  const explicit = configured?.trim()
  if (explicit) return explicit.replace(/\/$/, '')

  if (location && (location.protocol === 'http:' || location.protocol === 'https:')) {
    const url = new URL(location.origin)
    url.port = String(defaultPort)
    return url.origin
  }

  return `http://localhost:${defaultPort}`
}

