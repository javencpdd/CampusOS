import { describe, expect, it } from 'vitest'
import { buildCategoryNavigation } from '../src/campus-ui/categoryNavigation'

describe('category navigation', () => {
  it('keeps grouped boards behind one clickable group entry', () => {
    const result = buildCategoryNavigation([
      {
        id: 'group-test',
        name: '分组测试',
        node_kind: 'group',
        children: [
          { id: 'board-test', name: '测试板块', node_kind: 'board' },
          { id: 'board-moderator', name: '版主测试', node_kind: 'board' },
        ],
      },
      { id: 'board-root', name: '根级板块', node_kind: 'board' },
    ])

    expect(result).toHaveLength(2)
    expect(result[0]).toMatchObject({ kind: 'group', name: '分组测试' })
    expect(result[0].kind === 'group' ? result[0].children.map((item) => item.name) : []).toEqual([
      '测试板块',
      '版主测试',
    ])
    expect(result[1]).toMatchObject({ kind: 'board', name: '根级板块' })
  })

  it('omits archived, closed and empty group entries', () => {
    const result = buildCategoryNavigation([
      { id: 'empty', name: '空分组', node_kind: 'group', children: [] },
      { id: 'closed', name: '关闭板块', node_kind: 'board', is_closed: true },
      { id: 'archived', name: '归档板块', node_kind: 'board', lifecycle_status: 'archived' },
    ])
    expect(result).toEqual([])
  })
})
