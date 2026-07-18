export type PublicCategory = {
  id: string
  name: string
  node_kind?: 'group' | 'board'
  lifecycle_status?: 'active' | 'archived'
  is_closed?: boolean
  children?: PublicCategory[]
}

export type CategoryBoardNavigation = {
  key: string
  id: string
  name: string
  kind: 'board'
}

export type CategoryNavigationNode =
  | CategoryBoardNavigation
  | {
      key: string
      id: string
      name: string
      kind: 'group'
      children: CategoryBoardNavigation[]
    }

const activeBoard = (node: PublicCategory): CategoryBoardNavigation | null => {
  if ((node.node_kind || 'board') !== 'board' || node.is_closed || (node.lifecycle_status || 'active') !== 'active')
    return null
  return { key: `board:${node.id}`, id: node.id, name: node.name, kind: 'board' }
}

export const buildCategoryNavigation = (nodes: PublicCategory[]): CategoryNavigationNode[] => {
  const navigation: CategoryNavigationNode[] = []
  for (const node of nodes) {
    if ((node.lifecycle_status || 'active') !== 'active') continue
    if ((node.node_kind || 'board') === 'group') {
      const children = (node.children || [])
        .map(activeBoard)
        .filter((board): board is CategoryBoardNavigation => board !== null)
      if (children.length)
        navigation.push({ key: `group:${node.id}`, id: node.id, name: node.name, kind: 'group', children })
      continue
    }
    const board = activeBoard(node)
    if (board) navigation.push(board)
  }
  return navigation
}
