// normalizeThreadTags trims whitespace and deduplicates tags case-insensitively
// so list rows and detail pages render the same tag set for one thread.
export const normalizeThreadTags = (tags?: string[]): string[] => {
  const seen = new Set<string>()
  return (tags || []).reduce<string[]>((result, value) => {
    const tag = String(value || '').trim()
    const key = tag.toLocaleLowerCase()
    if (tag && !seen.has(key)) {
      seen.add(key)
      result.push(tag)
    }
    return result
  }, [])
}
