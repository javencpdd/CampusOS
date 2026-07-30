interface BrowserLocation {
  origin: string
  protocol: string
}

const isLoopbackHost = (hostname: string): boolean =>
  ['localhost', '127.0.0.1', '::1', '[::1]'].includes(hostname.toLowerCase())

export const resolveCompanionUrl = (
  configured: string | undefined,
  defaultPort: number,
  location: BrowserLocation | undefined = typeof window === 'undefined' ? undefined : window.location,
): string => {
  const explicit = configured?.trim()
  if (explicit) {
    const normalized = explicit.replace(/\/$/, '')
    if (location && (location.protocol === 'http:' || location.protocol === 'https:')) {
      try {
        const configuredUrl = new URL(normalized)
        const browserUrl = new URL(location.origin)
        if (isLoopbackHost(configuredUrl.hostname) && !isLoopbackHost(browserUrl.hostname)) {
          configuredUrl.hostname = browserUrl.hostname
          return configuredUrl.toString().replace(/\/$/, '')
        }
      } catch {
        return normalized
      }
    }
    return normalized
  }

  if (location && (location.protocol === 'http:' || location.protocol === 'https:')) {
    const url = new URL(location.origin)
    url.port = String(defaultPort)
    return url.origin
  }

  return `http://localhost:${defaultPort}`
}
