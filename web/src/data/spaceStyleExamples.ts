export interface StylePackage {
  manifest: {
    schema_version: string
    name: string
    version: string
    author?: string
    description?: string
    preview_image?: string
    compatible_campusos?: string[]
    layout: string
    components: Array<{
      slot: string
      type: string
      props?: Record<string, unknown>
    }>
    tokens?: Record<string, string>
    assets?: Array<{
      name: string
      path: string
      type: string
    }>
  }
}

export const styleExamples: StylePackage[] = [
  {
    manifest: {
      schema_version: 'space-style.v1',
      name: 'clean-blog',
      version: '0.1.0',
      author: 'CampusOS Team',
      description: 'A quiet blog layout for personal essays and study notes.',
      compatible_campusos: ['>=0.4.0'],
      layout: 'blog',
      components: [
        {
          slot: 'header',
          type: 'profile-header',
          props: { align: 'center', show_avatar: true, show_cover: false },
        },
        {
          slot: 'main',
          type: 'content-list',
          props: { density: 'comfortable', show_excerpt: true, show_meta: true },
        },
      ],
      tokens: {
        'color.primary': '#2563eb',
        'color.background': '#ffffff',
        'color.surface': '#f8fafc',
        'font.body': 'system-ui',
        'radius.card': '8px',
        'space.section': '24px',
      },
    },
  },
  {
    manifest: {
      schema_version: 'space-style.v1',
      name: 'grid-lab',
      version: '0.1.0',
      author: 'CampusOS Team',
      description: 'A compact grid layout for projects, labs and course collections.',
      compatible_campusos: ['>=0.4.0'],
      layout: 'grid',
      components: [
        {
          slot: 'header',
          type: 'profile-header',
          props: { align: 'left', show_avatar: true, show_cover: true },
        },
        {
          slot: 'main',
          type: 'category-tabs',
          props: { show_all: true },
        },
        {
          slot: 'main',
          type: 'content-list',
          props: { columns: 3, density: 'compact', show_excerpt: true },
        },
      ],
      tokens: {
        'color.primary': '#0891b2',
        'color.background': '#ecfeff',
        'color.surface': '#ffffff',
        'font.body': 'Inter',
        'radius.card': '6px',
        'space.section': '20px',
      },
    },
  },
  {
    manifest: {
      schema_version: 'space-style.v1',
      name: 'timeline-notes',
      version: '0.1.0',
      author: 'CampusOS Team',
      description: 'A timeline layout for logs, learning journals and release notes.',
      compatible_campusos: ['>=0.4.0'],
      layout: 'timeline',
      components: [
        {
          slot: 'header',
          type: 'profile-header',
          props: { align: 'left', show_avatar: true, show_cover: false },
        },
        {
          slot: 'main',
          type: 'content-list',
          props: { variant: 'timeline', show_excerpt: true, show_meta: true },
        },
        {
          slot: 'sidebar',
          type: 'tag-cloud',
          props: { max_items: 20 },
        },
      ],
      tokens: {
        'color.primary': '#4f46e5',
        'color.background': '#f5f3ff',
        'color.surface': '#ffffff',
        'font.body': 'system-ui',
        'radius.card': '10px',
        'space.section': '28px',
      },
    },
  },
  {
    manifest: {
      schema_version: 'space-style.v1',
      name: 'magazine-focus',
      version: '0.1.0',
      author: 'CampusOS Team',
      description: 'A magazine style for featured articles and curated personal homepages.',
      compatible_campusos: ['>=0.4.0'],
      layout: 'magazine',
      components: [
        {
          slot: 'header',
          type: 'hero',
          props: { height: 'medium', show_avatar: true },
        },
        {
          slot: 'main',
          type: 'content-list',
          props: { variant: 'featured', density: 'comfortable', show_excerpt: true },
        },
        {
          slot: 'footer',
          type: 'footer',
          props: { show_powered_by: true },
        },
      ],
      tokens: {
        'color.primary': '#be123c',
        'color.background': '#fff1f2',
        'color.surface': '#ffffff',
        'font.body': 'Georgia',
        'radius.card': '4px',
        'space.section': '32px',
      },
    },
  },
]
