# Plugin UI v2 Surface

> 更新时间：2026-09-15。

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
- 每次打开由宿主检查当前注册 Surface、加载状态和 Presentation；PDF 内容读取重新检查第一方插件状态、Invocation 和业务资源访问权。
- `builtin.pdf-viewer` 已接入 PDF Capability、管理员 Grant、用户 Consent 和插件版本/生命周期闭环；管理员或用户撤销后，后续创建和读取都会被拒绝。

## v1.1 PDF Viewer 的三个受控入口

`builtin.pdf-viewer` 是第一方受管插件，不是可上传或下载安装的 External Plugin 包。它以受信
`trusted-module` 注册 `builtin.pdf-viewer.preview`，由图文文章附件列表、个人空间“个人附件”和“我的文档”的
PDF 行共同调用同一 Surface：前者每次读取复核文章是否仍对当前用户开放；后两者每次读取复核 Asset 或 Personal
Document 是否仍属于当前用户。三种调用都只携带短期 Invocation ID，不会生成公开 URL；Personal Documents
只传递 document ID，External Plugin 不能复用 PDF.js trusted module 或注入动态前端 Bundle。

正式计划第 10.2 节允许编译期 trusted-module；`builtin` Runtime 只管理这项编译交付模块的 Manifest、生命周期
与三层授权，并不执行动态插件代码。PDF.js 深度代理、加载切换与关闭清理、浏览器存储受限、未注册 Surface 打开和
固定新标签页路径均已处理。编译产物采用内容哈希 URL，可由生产静态资源缓存复用；受保护 PDF 内容不进入持久缓存。
此处描述 PDF 专用 Invocation，不代表示例声明式 open-surface Action 的通用业务上下文签发已经交付。

## 开发和验收

插件持久数据仅允许使用平台已开放的现有通用表、自身 SQLite 和文件型 config。插件安装/升级不能执行平台建表或
改表 SQL；平台通用结构不足时由 CampusOS 版本统一演进。当前 `000001_v1_1_schema_baseline` 已含平台 Invocation 表及其三类资源上下文。
当前 process 使用明确环境白名单，不继承数据库、JWT、SMTP 或平台 `CAMPUSOS_*` 凭据；SQLite/config 使用私有布局，
PDF 最近页使用已存在的 `plugin_records` 用户命名空间而非 localStorage。OS/网络隔离与通用资源上下文服务尚未提取；
这些仍是最新存储约束下的待实施项。静态 JS/Worker 的 HTTP 缓存可继续保留。详见仓库
`docs/help/系统设计相关/插件存储边界与平台通用数据设计.md` 和正式计划第 7.0 节。

可参考仓库 `examples/plugins/v2-managed-example/plugin.yaml`。它包含一个可解析的 v2 Surface，且有测试保证示例
持续符合安装时使用的 Manifest 校验器。

```bash
go test ./internal/plugin -run 'TestManifestUIV2|TestV2ManagedExampleConformsToUIContract' -count=1
go run ./cmd/campusos-contracts --check
```

手工验收还应覆盖 v1 兼容、modal/drawer/fullscreen/new-tab、插件停用、授权撤销、调用过期和运行错误。真实浏览器
矩阵与目标 Linux 环境证据是版本发布门禁，不能用本地单元测试替代。
