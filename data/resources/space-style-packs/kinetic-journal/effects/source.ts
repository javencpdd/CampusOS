type StyleSDK = {
  request(method: 'space.profile.read' | 'space.posts.read', params?: Record<string, unknown>): Promise<unknown>
}

declare const CampusEffect: StyleSDK & {
  register(hooks: Record<string, (payload: any) => void>): void
}

// TypeScript authoring source. Compile to effects/main.js before packaging.
