import { defineConfig } from 'vitepress'

export default defineConfig({
  lang: 'zh-CN',
  title: 'CampusOS 官方文档',
  description: 'CampusOS 部署、接口、插件开发与系统设计文档',
  cleanUrls: true,
  lastUpdated: true,
  head: [
    ['meta', { name: 'theme-color', content: '#166534' }],
    ['meta', { name: 'color-scheme', content: 'light dark' }],
  ],
  markdown: {
    lineNumbers: true,
  },
  themeConfig: {
    siteTitle: 'CampusOS 文档',
    nav: [
      { text: '开始使用', link: '/guide/introduction' },
      { text: '部署', link: '/deployment/development' },
      { text: 'API', link: '/api/overview' },
      { text: '插件开发', link: '/plugins/overview' },
      { text: 'GitHub', link: 'https://github.com/javencpdd/CampusOS' },
    ],
    sidebar: [
      {
        text: '项目指南',
        items: [
          { text: '文档首页', link: '/' },
          { text: '项目介绍', link: '/guide/introduction' },
          { text: '系统架构', link: '/guide/architecture' },
          { text: '数据目录', link: '/reference/data-layout' },
        ],
      },
      {
        text: '安装与部署',
        items: [
          { text: '开发环境', link: '/deployment/development' },
          { text: '配置与端口', link: '/deployment/configuration' },
          { text: '构建与发布', link: '/deployment/release' },
        ],
      },
      {
        text: 'HTTP API',
        items: [
          { text: '接口约定', link: '/api/overview' },
          { text: '认证与社区', link: '/api/community' },
          { text: '版主管理', link: '/api/moderation' },
          { text: '当前契约与兼容', link: '/api/contracts' },
        ],
      },
      {
        text: '插件开发',
        items: [
          { text: '插件体系', link: '/plugins/overview' },
          { text: '编写第一个插件', link: '/plugins/create-first-plugin' },
          { text: 'Manifest 与配置', link: '/plugins/manifest' },
          { text: '打包、导入与更新', link: '/plugins/package-import' },
          { text: '生命周期与数据', link: '/plugins/lifecycle' },
          { text: '前端运行时与 Gateway', link: '/plugins/frontend-runtime' },
          { text: '风格包与沙箱 SDK', link: '/plugins/style-packs' },
          { text: 'Host API 与权限', link: '/plugins/host-api' },
		  { text: '插件中心、受管数据与签名', link: '/plugins/market-managed-data' },
          { text: 'SDK、CLI 与测试', link: '/plugins/sdk-cli' },
          { text: '版本兼容矩阵', link: '/plugins/compatibility' },
        ],
      },
      {
        text: '运维',
        items: [
          { text: '集成中心与能力边界', link: '/operations/integrations' },
          { text: '备份、恢复与验收', link: '/operations/recovery' },
        ],
      },
    ],
    search: {
      provider: 'local',
      options: {
        translations: {
          button: { buttonText: '搜索文档', buttonAriaLabel: '搜索文档' },
          modal: {
            noResultsText: '没有找到相关内容',
            resetButtonTitle: '清除搜索',
            footer: { selectText: '选择', navigateText: '切换', closeText: '关闭' },
          },
        },
      },
    },
    outline: { level: [2, 3], label: '本页目录' },
    docFooter: { prev: '上一篇', next: '下一篇' },
    lastUpdated: { text: '最后更新' },
    darkModeSwitchLabel: '外观',
    sidebarMenuLabel: '目录',
    returnToTopLabel: '返回顶部',
    externalLinkIcon: true,
    socialLinks: [
      { icon: 'github', link: 'https://github.com/javencpdd/CampusOS' },
    ],
    footer: {
      message: 'CampusOS 官方文档',
      copyright: 'Released under the repository license.',
    },
  },
})
