let nodeCount = 9

CampusEffect.register({
  start() {
    CampusEffect.request('community.threads.read', { limit: 10 })
      .then((result) => {
        const total = Number(result && result.total)
        if (Number.isFinite(total)) nodeCount = Math.max(6, Math.min(18, 6 + Math.round(total / 8)))
      })
      .catch(() => {})
  },
  frame(api) {
    const { ctx, width, height, dpr, time, pointer } = api
    api.clear()
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    ctx.lineWidth = 1
    for (let index = 0; index < nodeCount; index += 1) {
      const phase = time * 0.00008 + index * 0.73
      const x = width * (0.08 + ((index * 0.137 + Math.sin(phase) * 0.025) % 0.84))
      const y = height * (0.08 + ((index * 0.219 + Math.cos(phase * 1.3) * 0.018) % 0.84))
      const pullX = pointer.active ? (pointer.x * width - x) * 0.018 : 0
      const pullY = pointer.active ? (pointer.y * height - y) * 0.018 : 0
      ctx.beginPath()
      ctx.arc(x + pullX, y + pullY, index % 3 === 0 ? 2.4 : 1.4, 0, Math.PI * 2)
      ctx.fillStyle = index % 4 === 0 ? 'rgba(255,138,101,0.26)' : 'rgba(21,127,91,0.20)'
      ctx.fill()
    }
  },
})
