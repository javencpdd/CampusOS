import { defineConfig } from 'vitepress'

const includeLastUpdated = process.env.CAMPUSOS_DOCS_LAST_UPDATED !== 'false'

export default defineConfig({
  lang: 'zh-CN',
  title: 'CampusOS 官方文档',
  description: 'CampusOS 部署、接口、插件开发与系统设计文档',
  cleanUrls: true,
  lastUpdated: includeLastUpdated,
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
      { text: '开始使用', link: '/guide/developer-learning-path' },
      { text: '权限配置', link: '/guide/permission-configuration' },
      { text: '部署', link: '/deployment/development' },
      { text: 'API', link: '/api/overview' },
      { text: '插件开发', link: '/plugins/overview' },
      { text: '项目规划', link: '/project/current-roadmap' },
      { text: 'GitHub', link: 'https://github.com/javencpdd/CampusOS' },
    ],
    sidebar: [
      {
        text: '项目指南',
        items: [
          { text: '文档首页', link: '/' },
          { text: '开发者学习路线', link: '/guide/developer-learning-path' },
          { text: '完整入门路径', link: '/guide/getting-started' },
          { text: '权限配置入门', link: '/guide/permission-configuration' },
          { text: '验证码策略配置', link: '/guide/challenge-policy' },
          { text: '分组导航与图文发布', link: '/guide/structured-image-text' },
          { text: 'Web 交互、空间与通知', link: '/guide/web-ui-storage-notifications' },
          { text: '项目介绍', link: '/guide/introduction' },
          { text: '系统架构', link: '/guide/architecture' },
          { text: '模块与插件边界', link: '/guide/module-plugin-resource-boundaries' },
          { text: '数据目录', link: '/reference/data-layout' },
        ],
      },
      {
        text: '安装与部署',
        items: [
          { text: '开发环境', link: '/deployment/development' },
          { text: 'Docker 跨平台开发', link: '/deployment/docker-development' },
          { text: 'Docker 部署与迁移', link: '/deployment/docker' },
          { text: '配置与端口', link: '/deployment/configuration' },
          { text: '初始管理员安全', link: '/deployment/bootstrap-security' },
          { text: '邮箱账号升级', link: '/deployment/identity-account-migration' },
          { text: '验证码与 Ticket 安全', link: '/deployment/email-challenge-security' },
          { text: '验证码策略配置', link: '/guide/challenge-policy' },
          { text: '邮件投递与 SMTP', link: '/deployment/email-delivery' },
          { text: '会话与 Token 安全', link: '/api/session-security' },
          { text: '管理员准入与恢复', link: '/operations/admin-admission' },
          { text: '注册邮箱验证', link: '/api/registration' },
          { text: '账号恢复与邮箱绑定', link: '/api/account-recovery' },
          { text: '构建与发布', link: '/deployment/release' },
        ],
      },
      {
        text: 'HTTP API',
        items: [
          { text: '接口约定', link: '/api/overview' },
          { text: '注册邮箱验证', link: '/api/registration' },
          { text: '会话与 Token 安全', link: '/api/session-security' },
          { text: '多因素认证 API', link: '/api/mfa' },
          { text: '账号恢复与邮箱绑定', link: '/api/account-recovery' },
          { text: '认证与社区', link: '/api/community' },
          { text: '版主管理', link: '/api/moderation' },
          { text: '当前契约与兼容', link: '/api/contracts' },
        ],
      },
      {
        text: '插件开发',
        items: [
          { text: '插件体系', link: '/plugins/overview' },
          { text: '课表插件完整教程', link: '/plugins/schedule-plugin-tutorial' },
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
          { text: '可靠任务与 Webhook', link: '/operations/reliable-tasks' },
          { text: '管理员准入与紧急恢复', link: '/operations/admin-admission' },
          { text: '管理员 MFA 与恢复', link: '/operations/mfa' },
          { text: '容量基线与发布门禁', link: '/operations/capacity' },
          { text: '数据库迁移与 Schema 冗余', link: '/operations/database-migration-hygiene' },
          { text: '备份、恢复与验收', link: '/operations/recovery' },
        ],
      },
      {
        text: '参与贡献',
        items: [
          { text: '贡献、PR 与 CI/CD', link: '/contributing/workflow' },
          { text: '构建与发布', link: '/deployment/release' },
        ],
      },
      {
        text: '版本与规划',
        items: [
          { text: 'v0.1-v0.14 版本演进', link: '/project/version-evolution' },
          { text: '文档状态与历史替代', link: '/project/document-lifecycle' },
          { text: '当前规划与后续路线', link: '/project/current-roadmap' },
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
    lastUpdated: includeLastUpdated ? { text: '最后更新' } : false,
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
