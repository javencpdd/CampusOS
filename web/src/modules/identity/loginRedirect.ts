export function loginRedirect(value: unknown, origin = window.location.origin): string {
  if (
    typeof value !== 'string' ||
    !value.startsWith('/') ||
    value.startsWith('//') ||
    /[\\\u0000-\u001f\u007f]/.test(value)
  )
    return '/'
  try {
    const target = new URL(value, origin)
    return target.origin === origin && target.pathname !== '/login'
      ? target.pathname + target.search + target.hash
      : '/'
  } catch {
    return '/'
  }
}
