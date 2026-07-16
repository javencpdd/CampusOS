type EffectFrame = {
  ctx: OffscreenCanvasRenderingContext2D
  width: number
  height: number
  dpr: number
  time: number
  pointer: { x: number; y: number; active: boolean }
  clear(): void
}

declare const CampusEffect: {
  register(hooks: { start?(): void; frame?(api: EffectFrame): void }): void
  request(method: string, params?: Record<string, unknown>): Promise<unknown>
}

// TypeScript authoring source. Compile to effects/main.js before packaging.
