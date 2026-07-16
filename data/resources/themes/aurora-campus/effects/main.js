let count = 12

CampusEffect.register({
  start() {
    CampusEffect.request('community.threads.read', { limit: 12 })
      .then((result) => {
        const total = Number(result && result.total)
        if (Number.isFinite(total)) count = Math.max(8, Math.min(22, 8 + Math.round(total / 6)))
      })
      .catch(() => {})
  },
  frame(api) {
    const { ctx, width, height, dpr, time, pointer } = api
    api.clear()
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    for (let index = 0; index < count; index += 1) {
      const phase = time * 0.0001 + index * 0.67
      const baseX = width * (0.06 + ((index * 0.149) % 0.88))
      const baseY = height * (0.1 + ((index * 0.213) % 0.78))
      const x = baseX + Math.sin(phase) * 14 + (pointer.active ? (pointer.x * width - baseX) * 0.012 : 0)
      const y = baseY + Math.cos(phase * 1.2) * 10 + (pointer.active ? (pointer.y * height - baseY) * 0.012 : 0)
      ctx.beginPath()
      ctx.arc(x, y, index % 4 === 0 ? 2.2 : 1.2, 0, Math.PI * 2)
      ctx.fillStyle = index % 3 === 0 ? 'rgba(242,139,102,0.30)' : 'rgba(180,222,218,0.22)'
      ctx.fill()
    }
  },
})
