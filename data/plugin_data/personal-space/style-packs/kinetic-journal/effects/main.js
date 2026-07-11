let pointCount = 10

CampusEffect.register({
  start() {
    CampusEffect.request('space.posts.read', { limit: 20 })
      .then((result) => {
        const count = Array.isArray(result && result.items) ? result.items.length : 0
        pointCount = Math.max(8, Math.min(22, 8 + count))
      })
      .catch(() => {})
  },
  frame(api) {
    const { ctx, width, height, dpr, time, pointer } = api
    api.clear()
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    for (let index = 0; index < pointCount; index += 1) {
      const phase = index * 0.91 + time * 0.00012
      const baseX = width * (0.04 + ((index * 0.117) % 0.92))
      const baseY = height * (0.06 + ((index * 0.173) % 0.88))
      const x = baseX + Math.sin(phase) * 12 + (pointer.active ? (pointer.x * width - baseX) * 0.012 : 0)
      const y = baseY + Math.cos(phase * 0.8) * 10 + (pointer.active ? (pointer.y * height - baseY) * 0.012 : 0)
      const nextX = width * (0.04 + ((((index + 1) * 0.117) % 0.92)))
      const nextY = height * (0.06 + ((((index + 1) * 0.173) % 0.88)))
      ctx.beginPath()
      ctx.moveTo(x, y)
      ctx.lineTo(nextX, nextY)
      ctx.strokeStyle = 'rgba(20,33,28,0.055)'
      ctx.stroke()
      ctx.beginPath()
      ctx.arc(x, y, index % 4 === 0 ? 2.8 : 1.6, 0, Math.PI * 2)
      ctx.fillStyle = index % 3 === 0 ? 'rgba(255,138,101,0.30)' : 'rgba(27,111,85,0.22)'
      ctx.fill()
    }
  },
})
