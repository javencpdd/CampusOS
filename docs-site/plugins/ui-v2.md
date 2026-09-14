# Plugin UI v2 Surface

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
- 每次打开和内容读取都重新检查插件状态、Capability、管理员 Grant、用户 Consent 和业务资源访问权；停用、撤销
  或调用过期后，旧页面不能继续读取内容。

## v1.1 PDF Viewer 的两个受控入口

`feature.pdf-viewer` 是第一方 Built-in Feature，而不是可下载安装的 External Plugin。它以受信
`trusted-module` 注册 `builtin.pdf-viewer.preview`，由图文文章附件列表和个人空间“个人附件”共同调用同一
Surface：前者每次读取复核文章是否仍对当前用户开放，后者每次读取复核 Asset 是否仍属于当前用户。两种调用都只携带
短期 Invocation ID，不会生成公开 URL；External Plugin 不能复用 PDF.js trusted module 或注入动态前端 Bundle。

## 开发和验收

可参考仓库 `examples/plugins/v2-managed-example/plugin.yaml`。它包含一个可解析的 v2 Surface，且有测试保证示例
持续符合安装时使用的 Manifest 校验器。

```bash
go test ./internal/plugin -run 'TestManifestUIV2|TestV2ManagedExampleConformsToUIContract' -count=1
go run ./cmd/campusos-contracts --check
```

手工验收还应覆盖 v1 兼容、modal/drawer/fullscreen/new-tab、插件停用、授权撤销、调用过期和运行错误。真实浏览器
矩阵与目标 Linux 环境证据是版本发布门禁，不能用本地单元测试替代。
