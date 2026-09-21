# Plugin UI v2 Surface

> 更新时间：2026-09-22（Asia/Shanghai）。
> 适用范围：本文保留 `campusos.ui/v2` 的旧声明式合同说明。当前 PDF Viewer 已迁至 `campusos.plugin/v4` 自包含包并使用 `campusos.ui/v3` 隔离 iframe；请优先阅读[插件重构计划](../../docs/项目计划书v1/项目计划v1.1/02-v1.1插件自包含与动态加载重构计划书.md)和[当前架构](../../docs/architecture/模块设计/插件平台与授权体系.md)。

`campusos.ui/v2` 为插件提供受宿主管理的界面打开能力。它适合 PDF 预览、受管数据详情等需要弹窗、抽屉、全屏或
同源新标签页的场景，但不会把浏览器、主站路由或登录凭据控制权交给插件。

## 声明一个 Surface

```yaml
ui:
  contract_version: campusos.ui/v2
  actions:
    - id: my-plugin.open-preview
      kind: open-surface
      surface_id: my-plugin.preview
      presentation: modal
  surfaces:
    - id: my-plugin.preview
      version: v1
      type: record-preview
      layout_role: overlay
      renderer: schema
      schema: { component: stack }
      presentations: [modal, drawer, fullscreen, new-tab]
```

插件请求的是已声明的 `surface_id`，而不是 URL。CampusOS 根据当前设备、页面和安全策略选择实际展示方式；请求的
`presentation` 必须出现在 Surface 的 `presentations` 白名单中。

## 安全边界

- `open-surface` 不能带 `path`、`method`、URL、HTML 或脚本，插件不能直接 `window.open()`。
- External Plugin 默认使用声明式 `schema`；只有随 CampusOS 编译且被信任白名单允许的 Built-in Feature 能使用
  `trusted-module`。
- 同源新标签页只能使用短期、服务端保存的调用上下文，不能把 JWT、对象路径、存储 key 或长期下载链接写入 URL。
- 每次打开由宿主检查当前注册 Surface、加载状态和 Presentation；PDF 内容读取重新检查当前外部插件 release、Invocation 和业务资源访问权。
- `builtin.pdf-viewer` 是已移除的历史实现，不能再被安装、展示或授权。当前 PDF Viewer 是自包含外部插件 `campusos.pdf-viewer`，使用 v4 包和 v3 隔离 iframe；管理员或用户撤销后，后续创建和读取都会被拒绝。

## v1.1 PDF Viewer 的三个受控入口

`campusos.pdf-viewer` 是当前唯一 PDF 预览外部插件。它以校验后的 v4 release 注册
`campusos.pdf-viewer.preview`，由图文文章附件列表、个人空间“个人附件”和“我的文档”的
PDF 行共同调用同一 Surface：前者每次读取复核文章是否仍对当前用户开放；后两者每次读取复核 Asset 或 Personal
Document 是否仍属于当前用户。三种调用都只携带短期 Invocation ID，不会生成公开 URL；Personal Documents
只传递 document ID，External Plugin 不能复用 PDF.js trusted module 或注入动态前端 Bundle。

`trusted-module` 和 `builtin` Runtime 的说明只适用于旧 v1/v2 兼容实现，不能用于新的 PDF Viewer。
当前 PDF.js 产物随 `campusos.pdf-viewer` v4 release 发布到独立 Origin，可由浏览器缓存；受保护的 PDF 内容不进入
持久缓存。通用签发仍由 `internal/platform/pluginui` 完成：只有宿主用户选择产生的 `resourceContext` 才能签发，
不会从 Action body 信任文件路径或 owner。当前支持的资源仍只有三类 PDF 上下文，不提供 Office/音视频预览。

v4 网关只服务 release 自身的 `/plugins/<key>/<version>-<digest>/...` 路径。插件 Vite 构建使用相对 `base: './'`，
打包器会把入口目录和共享 `dist/assets/` 一并发布到 `ui/` 下；这样 PDF.js Worker 和 Bridge 脚本不会错误请求主站根目录
`/assets/`。若页面停在“正在连接 CampusOS…”，应先检查入口和共享资源是否均在该 release 内返回 `200`，这发生在授权校验之前。

## 通用签发与下载降级

宿主在用户点击时调用 `POST /api/v1/plugin-ui/invocations`：

```json
{
  "plugin_key": "my-plugin",
  "action_id": "my-plugin.open-preview",
  "surface_id": "my-plugin.preview",
  "presentation": "modal",
  "resource_type": "personal_asset",
  "resource_id": "123"
}
```

文章附件使用 `resource_type=article_attachment`、附件绑定 ID 与额外的 `thread_id`；个人文档使用
`personal_document` 和文档 ID。所有 ID 使用字符串。服务端验证已安装且运行的插件声明、Action、展示方式、
业务 ACL 和 Grant/Consent，返回短期 Invocation；读取时摘要校验插件与对象版本，版本变化须重新打开。

受限文件读取仍须用户同意。普通下载是独立宿主权限：Viewer 下载按钮调用
`GET /api/v1/plugin-ui/invocations/:id/download`，即使预览过期或插件停用也重新按文章 ACL/owner 判断。
它不恢复预览授权；移除附件会原子删除其上下文，过期超过 24 小时的上下文会有界清理，此后从来源页面下载。

## 开发和验收

插件持久数据仅允许使用平台已开放的现有通用表、自身 SQLite 和文件型 config。插件安装/升级不能执行平台建表或
改表 SQL；平台通用结构不足时由 CampusOS 版本统一演进。当前 `000001_v1_1_schema_baseline` 已含平台 Invocation 表及其三类资源上下文。
当前 process 使用明确环境白名单，不继承数据库、JWT、SMTP 或平台 `CAMPUSOS_*` 凭据；SQLite/config 使用私有布局，
PDF 最近页使用已存在的 `plugin_records` 用户命名空间而非 localStorage。通用上下文服务已提取；
OS/目录/数据库网络隔离仍须目标部署实施与验证，不能把环境白名单称为沙箱。静态 JS/Worker 的 HTTP 缓存可继续保留。详见仓库
`docs/help/系统设计相关/插件存储边界与平台通用数据设计.md` 和正式计划第 7.0 节。

可参考仓库 `examples/plugins/v2-managed-example/plugin.yaml`。它包含一个可解析的 v2 Surface，且有测试保证示例
持续符合安装时使用的 Manifest 校验器。

```bash
go test ./internal/plugin -run 'TestManifestUIV2|TestV2ManagedExampleConformsToUIContract' -count=1
go run ./cmd/campusos-contracts --check
```

手工验收还应覆盖 v1 兼容、modal/drawer/fullscreen/new-tab、插件停用、授权撤销、调用过期和运行错误。真实浏览器
矩阵与目标 Linux 环境证据是版本发布门禁，不能用本地单元测试替代。
